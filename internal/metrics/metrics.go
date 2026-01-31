package metrics

import (
	"net/http"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	zoneReloadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpz_loader_zone_reload_total",
			Help: "Total number of zone reloads attempted.",
		},
		[]string{"zone", "result"},
	)

	zoneReloadDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rpz_loader_zone_reload_duration_seconds",
			Help:    "Duration of zone reloads.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"zone"},
	)
)

func init() {
	prometheus.MustRegister(zoneReloadTotal)
	prometheus.MustRegister(zoneReloadDurationSeconds)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

type PrometheusMonitor struct {
	jobsCompleted *prometheus.CounterVec
	jobTiming     *prometheus.HistogramVec
}

func NewPrometheusMonitor() *PrometheusMonitor {
	return &PrometheusMonitor{
		jobsCompleted: zoneReloadTotal,
		jobTiming:     zoneReloadDurationSeconds,
	}
}

func (p *PrometheusMonitor) IncrementJob(id uuid.UUID, name string, tags []string, status gocron.JobStatus) {
	p.jobsCompleted.WithLabelValues(name, string(status)).Inc()
}

func (p *PrometheusMonitor) RecordJobTiming(startTime, endTime time.Time, id uuid.UUID, name string, tags []string) {
	p.jobTiming.WithLabelValues(name).Observe(endTime.Sub(startTime).Seconds())
}
