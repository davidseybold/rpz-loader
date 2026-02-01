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
		if r.Type == string(config.RPZTypeStatic) {
			_, err = s.NewJob(
				gocron.OneTimeJob(
					gocron.OneTimeJobStartDateTime(time.Now().Add(10*time.Second)),
				),
				gocron.NewTask(syncStaticRPZ, logger, zoneFile, r.Name, r.TTL, r.Rules),
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
					gocron.NewTask(syncZoneFromRemote, logger, zoneFile, r.Name, r.URL, cfg.DryRun),
					gocron.WithName(r.Name),
				)
				if err != nil {
					return err
				}
			}

			_, err = s.NewJob(
				gocron.CronJob(r.ReloadSchedule, false),
				gocron.NewTask(syncZoneFromRemote, logger, zoneFile, r.Name, r.URL, cfg.DryRun),
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

func syncZoneFromRemote(logger *slog.Logger, zoneFile string, zoneName string, url string, dryRun bool) {
	logger.Info("Syncing zone from remote", "zone", zoneName, "url", url)

	err := rpz.FetchZoneFile(zoneFile, zoneName, url)
	if err != nil {
		logger.Error("Failed to fetch zone contents from remote", "zone", zoneName, "error", err)
		return
	}

	if dryRun {
		logger.Info("[DRY RUN] Zone synced from remote", "zone", zoneName)
		return
	}

	err = powerdns.SyncZoneFromFile(zoneName, zoneFile)
	if err != nil {
		logger.Error("Failed to sync zone from file to PowerDNS", "zone", zoneName, "error", err)
		return
	}

	logger.Info("Zone synced from remote", "zone", zoneName)
}

func syncStaticRPZ(logger *slog.Logger, zoneFile string, zoneName string, ttl int, rules []config.RPZRule, dryRun bool) {
	logger.Info("Syncing static RPZ zone", "zone", zoneName)

	err := rpz.WriteZoneFile(zoneFile, zoneName, ttl, rules)
	if err != nil {
		logger.Error("Failed to write zone file", "zone", zoneName, "error", err)
		return
	}

	if dryRun {
		logger.Info("[DRY RUN] Static RPZ zone synced", "zone", zoneName)
		return
	}

	err = powerdns.SyncZoneFromFile(zoneName, zoneFile)
	if err != nil {
		logger.Error("Failed to sync static RPZ zone", "zone", zoneName, "error", err)
		return
	}

	logger.Info("Static RPZ zone synced", "zone", zoneName)
}

func zoneFileName(dataDir string, zoneName string) string {
	return filepath.Join(dataDir, zoneName+".zone")
}
