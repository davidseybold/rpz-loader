package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/davidseybold/rpz-loader/internal/config"
	"github.com/davidseybold/rpz-loader/internal/metrics"
	"github.com/davidseybold/rpz-loader/internal/powerdns"
	"github.com/davidseybold/rpz-loader/internal/rpz"
	"github.com/go-co-op/gocron/v2"
	"github.com/oklog/run"
)

func Run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())

	metricsServer := &http.Server{
		Addr:    ":2112",
		Handler: mux,
	}

	s, err := gocron.NewScheduler(gocron.WithMonitor(metrics.NewPrometheusMonitor()), gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait))
	if err != nil {
		return err
	}

	err = addJobs(s, cfg, logger, newSyncer(logger, pdnsStore{}, cfg.AlsoNotify, cfg.DryRun))
	if err != nil {
		return err
	}

	var g run.Group

	{
		server := metricsServer
		g.Add(
			func() error {
				logger.Info("metrics server starting", "addr", server.Addr)
				err := server.ListenAndServe()
				if err == http.ErrServerClosed {
					return nil
				}
				return err
			},
			func(_ error) {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := server.Shutdown(shutdownCtx); err != nil {
					logger.Error("failed to shutdown metrics server", "error", err)
				}
			},
		)
	}

	{
		scheduler := s
		stopCh := make(chan struct{})
		g.Add(
			func() error {
				logger.Info("scheduler starting")
				scheduler.Start()
				<-stopCh
				return nil
			},
			func(_ error) {
				close(stopCh)
				if err := scheduler.Shutdown(); err != nil {
					logger.Error("failed to shutdown scheduler", "error", err)
				}
			},
		)
	}

	g.Add(run.SignalHandler(context.Background(), os.Interrupt, syscall.SIGTERM))

	return g.Run()
}

func addJobs(s gocron.Scheduler, cfg *config.Config, logger *slog.Logger, sy *syncer) error {
	for _, r := range cfg.RPZs {
		zoneFile := zoneFileName(cfg.DataDir, r.Name)
		rpzOpts := rpz.Opts{
			Filename:        zoneFile,
			Type:            r.Type,
			ZoneName:        r.Name,
			Nameserver:      cfg.Nameserver,
			HostmasterEmail: cfg.HostmasterEmail,
			TTL:             r.TTL,
			Refresh:         r.Refresh,
			Retry:           r.Retry,
			Expire:          r.Expire,
			NegativeTTL:     r.NegativeTTL,
		}

		if r.Type == string(config.RPZTypeStatic) {
			_, err := s.NewJob(
				gocron.OneTimeJob(
					gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
				),
				gocron.NewTask(sy.run, staticFileBuilder(r.Rules), rpzOpts),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		} else {
			fetch := remoteFileFetcher(r.URL, r.MinRules, r.MaxShrinkPercent)
			if r.FetchOnStart {
				_, err := s.NewJob(
					gocron.OneTimeJob(
						gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
					),
					gocron.NewTask(sy.run, fetch, rpzOpts),
					gocron.WithName(r.Name),
				)
				if err != nil {
					return err
				}
			}

			_, err := s.NewJob(
				gocron.CronJob(r.ReloadSchedule, false),
				gocron.NewTask(sy.run, fetch, rpzOpts),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// fileBuilder writes a zone's file and returns its rule count; previous is the
// rule count of the last good version (0 when unknown).
type fileBuilder func(rpzOpts rpz.Opts, previous int) (int, error)

// zoneStore loads zones into the authoritative server.
type zoneStore interface {
	Load(zone, file string) error
	SetAlsoNotify(zone string, hosts []string) error
	Notify(zone string) error
}

type pdnsStore struct{}

func (pdnsStore) Load(zone, file string) error { return powerdns.SyncZoneFromFile(zone, file) }
func (pdnsStore) SetAlsoNotify(zone string, hosts []string) error {
	return powerdns.SetMetadataAlsoNotify(zone, hosts)
}
func (pdnsStore) Notify(zone string) error { return powerdns.NotifyZone(zone) }

// syncer runs zone syncs. A failed sync returns its error, so the scheduler counts
// it (rpz_loader_zone_reload_total{result="fail"}), and leaves the loaded zone as is.
type syncer struct {
	logger     *slog.Logger
	store      zoneStore
	alsoNotify []string
	dryRun     bool
	now        func() time.Time

	mu       sync.Mutex
	previous map[string]int // rule count of each zone's last good file
}

func newSyncer(logger *slog.Logger, store zoneStore, alsoNotify []string, dryRun bool) *syncer {
	return &syncer{logger: logger, store: store, alsoNotify: alsoNotify, dryRun: dryRun, now: time.Now, previous: map[string]int{}}
}

func (sy *syncer) run(buildFile fileBuilder, rpzOpts rpz.Opts) error {
	zone := rpzOpts.ZoneName
	sy.logger.Info("Syncing RPZ zone", "zone", zone)

	previous, err := sy.previousRules(rpzOpts)
	if err != nil {
		sy.logger.Warn("Could not count the rules of the current zone file", "zone", zone, "error", err)
	}

	rules, err := buildFile(rpzOpts, previous)
	if err != nil {
		sy.logger.Error("Failed to build RPZ file", "zone", zone, "error", err)
		return fmt.Errorf("build %s: %w", zone, err)
	}

	if sy.dryRun {
		sy.logger.Info("[DRY RUN] RPZ zone synced", "zone", zone, "rules", rules)
		return nil
	}

	if err := sy.store.Load(zone, rpzOpts.Filename); err != nil {
		sy.logger.Error("Failed to sync RPZ zone to PowerDNS", "zone", zone, "error", err)
		return fmt.Errorf("load %s: %w", zone, err)
	}

	if err := sy.store.SetAlsoNotify(zone, sy.alsoNotify); err != nil {
		sy.logger.Error("Failed to set also notify for RPZ zone", "zone", zone, "error", err)
		return fmt.Errorf("set also-notify for %s: %w", zone, err)
	}

	if err := sy.store.Notify(zone); err != nil {
		sy.logger.Error("Failed to notify RPZ zone", "zone", zone, "error", err)
		return fmt.Errorf("notify %s: %w", zone, err)
	}

	sy.mu.Lock()
	sy.previous[zone] = rules
	sy.mu.Unlock()
	metrics.RecordSuccess(zone, rpzOpts.Type, rules, sy.now())
	sy.logger.Info("RPZ zone synced", "zone", zone, "rules", rules)
	return nil
}

// previousRules is the last good rule count, from memory or, after a restart,
// from the zone file on disk.
func (sy *syncer) previousRules(rpzOpts rpz.Opts) (int, error) {
	sy.mu.Lock()
	n, ok := sy.previous[rpzOpts.ZoneName]
	sy.mu.Unlock()
	if ok {
		return n, nil
	}
	return rpz.CountRules(rpzOpts.Filename)
}

func remoteFileFetcher(url string, minRules, maxShrinkPercent int) fileBuilder {
	return func(rpzOpts rpz.Opts, previous int) (int, error) {
		guard := rpz.Guard{MinRules: minRules, MaxShrinkPercent: maxShrinkPercent, Previous: previous}
		n, err := rpz.FetchZoneFile(rpzOpts, url, guard)
		if err != nil {
			return n, fmt.Errorf("fetch %s: %w", url, err)
		}
		return n, nil
	}
}

func staticFileBuilder(rules []config.RPZRule) fileBuilder {
	return func(rpzOpts rpz.Opts, _ int) (int, error) {
		return rpz.WriteZoneFileFromRules(rpzOpts, rules)
	}
}

func zoneFileName(dataDir string, zoneName string) string {
	return filepath.Join(dataDir, zoneName+".zone")
}
