package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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

	err = addJobs(s, cfg, logger)
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

func addJobs(s gocron.Scheduler, cfg *config.Config, logger *slog.Logger) error {
	for _, r := range cfg.RPZs {
		zoneFile := zoneFileName(cfg.DataDir, r.Name)
		rpzOpts := rpz.Opts{
			Filename:        zoneFile,
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
				gocron.NewTask(runSync, logger, staticFileBuilder(r.Rules), rpzOpts, cfg.AlsoNotify, cfg.DryRun),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		} else {
			if r.FetchOnStart {
				_, err := s.NewJob(
					gocron.OneTimeJob(
						gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
					),
					gocron.NewTask(runSync, logger, remoteFileFetcher(r.URL), rpzOpts, cfg.AlsoNotify, cfg.DryRun),
					gocron.WithName(r.Name),
				)
				if err != nil {
					return err
				}
			}

			_, err := s.NewJob(
				gocron.CronJob(r.ReloadSchedule, false),
				gocron.NewTask(runSync, logger, remoteFileFetcher(r.URL), rpzOpts, cfg.AlsoNotify, cfg.DryRun),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

type fileBuilder func(rpzOpts rpz.Opts) error

func runSync(logger *slog.Logger, buildFile fileBuilder, rpzOpts rpz.Opts, alsoNotify string, dryRun bool) {
	logger.Info("Syncing RPZ zone", "zone", rpzOpts.ZoneName)

	err := buildFile(rpzOpts)
	if err != nil {
		logger.Error("Failed to build RPZ file", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	if dryRun {
		logger.Info("[DRY RUN] RPZ zone synced", "zone", rpzOpts.ZoneName)
		return
	}

	err = powerdns.SyncZoneFromFile(rpzOpts.ZoneName, rpzOpts.Filename)
	if err != nil {
		logger.Error("Failed to sync RPZ zone to PowerDNS", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	err = powerdns.SetMetadataAlsoNotify(rpzOpts.ZoneName, alsoNotify)
	if err != nil {
		logger.Error("Failed to set also notify for RPZ zone", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	logger.Info("RPZ zone synced", "zone", rpzOpts.ZoneName)
}

func remoteFileFetcher(url string) func(rpzOpts rpz.Opts) error {
	return func(rpzOpts rpz.Opts) error {
		err := rpz.FetchZoneFile(rpzOpts, url)
		if err != nil {
			return fmt.Errorf("failed to fetch zone contents from remote %s: %w", url, err)
		}
		return nil
	}
}

func staticFileBuilder(rules []config.RPZRule) func(rpzOpts rpz.Opts) error {
	return func(rpzOpts rpz.Opts) error {
		err := rpz.WriteZoneFileFromRules(rpzOpts, rules)
		if err != nil {
			return fmt.Errorf("failed to write zone file %s: %w", rpzOpts.Filename, err)
		}
		return nil
	}
}

func zoneFileName(dataDir string, zoneName string) string {
	return filepath.Join(dataDir, zoneName+".zone")
}
