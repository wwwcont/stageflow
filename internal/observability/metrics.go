package observability

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"stageflow/internal/domain"
)

type Metrics struct {
	Registry *prometheus.Registry

	flowRunsTotal              *prometheus.CounterVec
	flowRunDurationSeconds     *prometheus.HistogramVec
	flowStepDurationSeconds    *prometheus.HistogramVec
	flowStepFailuresTotal      *prometheus.CounterVec
	activeRuns                 prometheus.Gauge
	httpRequestsTotal          *prometheus.CounterVec
	httpRequestDurationSeconds *prometheus.HistogramVec
	workerJobsTotal            *prometheus.CounterVec
	activeRunsValue            atomic.Int64
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		Registry: registry,
		flowRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flow_runs_total",
			Help: "Total number of flow runs that reached a terminal state.",
		}, []string{"status", "target_type"}),
		flowRunDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "flow_run_duration_seconds",
			Help:    "Observed duration of completed flow runs.",
			Buckets: prometheus.DefBuckets,
		}, []string{"status", "target_type"}),
		flowStepDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "flow_step_duration_seconds",
			Help:    "Observed duration of flow step attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"step", "status"}),
		flowStepFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "flow_step_failures_total",
			Help: "Total number of failed flow step attempts grouped by failure kind.",
		}, []string{"step", "failure_kind"}),
		activeRuns: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "active_runs",
			Help: "Number of runs currently executing.",
		}),
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests served by the API.",
		}, []string{"method", "route", "status"}),
		httpRequestDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Observed duration of HTTP requests served by the API.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		workerJobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "worker_jobs_total",
			Help: "Total worker job lifecycle events.",
		}, []string{"queue", "event"}),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.flowRunsTotal,
		m.flowRunDurationSeconds,
		m.flowStepDurationSeconds,
		m.flowStepFailuresTotal,
		m.activeRuns,
		m.httpRequestsTotal,
		m.httpRequestDurationSeconds,
		m.workerJobsTotal,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveRequest(_ context.Context, method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	statusLabel := strconv.Itoa(status)
	route = sanitizeRoute(route)
	m.httpRequestsTotal.WithLabelValues(strings.ToUpper(method), route, statusLabel).Inc()
	m.httpRequestDurationSeconds.WithLabelValues(strings.ToUpper(method), route, statusLabel).Observe(duration.Seconds())
}

func (m *Metrics) RecordStepAttempt(step string, duration time.Duration, err error, failureKind string) {
	if m == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "failure"
		m.flowStepFailuresTotal.WithLabelValues(sanitizeLabel(step), sanitizeLabel(failureKind)).Inc()
	}
	m.flowStepDurationSeconds.WithLabelValues(sanitizeLabel(step), status).Observe(duration.Seconds())
}

func (m *Metrics) RecordWorkerJob(queue, event string) {
	if m == nil {
		return
	}
	m.workerJobsTotal.WithLabelValues(sanitizeLabel(queue), sanitizeLabel(event)).Inc()
}

func (m *Metrics) TrackRunCreated(run domain.FlowRun) {
	if m == nil {
		return
	}
	if run.Status == domain.RunStatusRunning {
		m.bumpActiveRuns(1)
	}
	if run.IsTerminal() {
		m.recordTerminalRun(run)
	}
}

func (m *Metrics) TrackRunTransition(before, after domain.FlowRun) {
	if m == nil {
		return
	}
	if before.Status != domain.RunStatusRunning && after.Status == domain.RunStatusRunning {
		m.bumpActiveRuns(1)
	}
	if before.Status == domain.RunStatusRunning && after.Status != domain.RunStatusRunning {
		m.bumpActiveRuns(-1)
	}
	if !before.IsTerminal() && after.IsTerminal() {
		m.recordTerminalRun(after)
	}
}

func (m *Metrics) bumpActiveRuns(delta int64) {
	current := m.activeRunsValue.Add(delta)
	if current < 0 {
		m.activeRunsValue.Store(0)
		current = 0
	}
	m.activeRuns.Set(float64(current))
}

func (m *Metrics) recordTerminalRun(run domain.FlowRun) {
	status := sanitizeLabel(string(run.Status))
	targetType := sanitizeLabel(string(run.Target()))
	m.flowRunsTotal.WithLabelValues(status, targetType).Inc()
	startedAt := run.CreatedAt
	if run.StartedAt != nil && !run.StartedAt.IsZero() {
		startedAt = run.StartedAt.UTC()
	}
	finishedAt := run.UpdatedAt
	if run.FinishedAt != nil && !run.FinishedAt.IsZero() {
		finishedAt = run.FinishedAt.UTC()
	}
	if finishedAt.After(startedAt) {
		m.flowRunDurationSeconds.WithLabelValues(status, targetType).Observe(finishedAt.Sub(startedAt).Seconds())
	}
}

func sanitizeRoute(route string) string {
	trimmed := strings.TrimSpace(route)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func sanitizeLabel(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
