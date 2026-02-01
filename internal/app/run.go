package app

import (
	"context"
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

	for _, r := range cfg.RPZs {
		zoneFile := zoneFileName(cfg.DataDir, r.Name)
		rpzOpts := rpz.Opts{
			Filename:        zoneFile,
			ZoneName:        r.Name,
			Nameserver:      cfg.Nameserver,
			HostmasterEmail: cfg.HostmasterEmail,
			TTL:             r.TTL,
		}

		if r.Type == string(config.RPZTypeStatic) {
			_, err = s.NewJob(
				gocron.OneTimeJob(
					gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
				),
				gocron.NewTask(syncStaticRPZ, logger, rpzOpts, r.Rules, cfg.DryRun),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		} else {
			if r.FetchOnStart {
				_, err = s.NewJob(
					gocron.OneTimeJob(
						gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
					),
					gocron.NewTask(syncZoneFromRemote, logger, rpzOpts, r.URL, cfg.DryRun),
					gocron.WithName(r.Name),
				)
				if err != nil {
					return err
				}
			}

			_, err = s.NewJob(
				gocron.CronJob(r.ReloadSchedule, false),
				gocron.NewTask(syncZoneFromRemote, logger, rpzOpts, r.URL, cfg.DryRun),
				gocron.WithName(r.Name),
			)
			if err != nil {
				return err
			}
		}
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

func syncZoneFromRemote(logger *slog.Logger, rpzOpts rpz.Opts, url string, dryRun bool) {
	logger.Info("Syncing zone from remote", "zone", rpzOpts.ZoneName, "url", url)

	err := rpz.FetchZoneFile(rpzOpts, url)
	if err != nil {
		logger.Error("Failed to fetch zone contents from remote", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	if dryRun {
		logger.Info("[DRY RUN] Zone synced from remote", "zone", rpzOpts.ZoneName)
		return
	}

	err = powerdns.SyncZoneFromFile(rpzOpts.ZoneName, rpzOpts.Filename)
	if err != nil {
		logger.Error("Failed to sync zone from file to PowerDNS", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	logger.Info("Zone synced from remote", "zone", rpzOpts.ZoneName)
}

func syncStaticRPZ(logger *slog.Logger, rpzOpts rpz.Opts, rules []config.RPZRule, dryRun bool) {
	logger.Info("Syncing static RPZ zone", "zone", rpzOpts.ZoneName)

	err := rpz.WriteZoneFileFromRules(rpzOpts, rules)
	if err != nil {
		logger.Error("Failed to write zone file", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	if dryRun {
		logger.Info("[DRY RUN] Static RPZ zone synced", "zone", rpzOpts.ZoneName)
		return
	}

	err = powerdns.SyncZoneFromFile(rpzOpts.ZoneName, rpzOpts.Filename)
	if err != nil {
		logger.Error("Failed to sync static RPZ zone", "zone", rpzOpts.ZoneName, "error", err)
		return
	}

	logger.Info("Static RPZ zone synced", "zone", rpzOpts.ZoneName)
}

func zoneFileName(dataDir string, zoneName string) string {
	return filepath.Join(dataDir, zoneName+".zone")
}
