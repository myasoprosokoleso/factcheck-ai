package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	TelegramUpdatesTotal  *prometheus.CounterVec
	TelegramCommentsTotal *prometheus.CounterVec

	FactCheckJobsTotal       *prometheus.CounterVec
	FactCheckDurationSeconds prometheus.Histogram
	FactCheckOutcomeTotal    *prometheus.CounterVec

	registry *prometheus.Registry
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		TelegramUpdatesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "telegram_updates_total", Help: "Telegram updates received by kind.",
		}, []string{"kind"}),
		TelegramCommentsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "telegram_comments_total", Help: "Telegram comment deliveries by status.",
		}, []string{"status"}),
		FactCheckJobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "factcheck_jobs_total", Help: "Fact-check jobs by outcome.",
		}, []string{"status"}),
		FactCheckDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "factcheck_duration_seconds", Help: "End-to-end fact-check latency.", Buckets: prometheus.DefBuckets,
		}),
		FactCheckOutcomeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "factcheck_outcome_total", Help: "Fact-check results by outcome.",
		}, []string{"outcome"}),
		registry: registry,
	}
	registry.MustRegister(
		m.TelegramUpdatesTotal, m.TelegramCommentsTotal,
		m.FactCheckJobsTotal, m.FactCheckDurationSeconds, m.FactCheckOutcomeTotal,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
