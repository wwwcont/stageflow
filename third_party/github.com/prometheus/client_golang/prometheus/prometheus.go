package prometheus

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Collector interface{ Collect() []string }
type Registry struct {
	mu         sync.RWMutex
	collectors []Collector
}

func NewRegistry() *Registry { return &Registry{} }
func (r *Registry) MustRegister(cs ...Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, cs...)
}
func (r *Registry) Gather() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, c := range r.collectors {
		out = append(out, c.Collect()...)
	}
	return out
}

type CounterOpts struct{ Name, Help string }
type HistogramOpts struct {
	Name, Help string
	Buckets    []float64
}
type GaugeOpts struct{ Name, Help string }

var DefBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type counterValue struct {
	labels []string
	value  float64
}
type CounterVec struct {
	mu sync.Mutex
	CounterOpts
	labelNames []string
	values     map[string]*counterValue
}
type Counter interface{ Inc() }
type counterHandle struct {
	vec *CounterVec
	key string
}

func NewCounterVec(opts CounterOpts, labelNames []string) *CounterVec {
	return &CounterVec{CounterOpts: opts, labelNames: labelNames, values: map[string]*counterValue{}}
}
func (v *CounterVec) WithLabelValues(vals ...string) Counter {
	key := strings.Join(vals, "\xff")
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.values[key]; !ok {
		v.values[key] = &counterValue{labels: append([]string(nil), vals...)}
	}
	return counterHandle{vec: v, key: key}
}
func (h counterHandle) Inc() { h.vec.mu.Lock(); defer h.vec.mu.Unlock(); h.vec.values[h.key].value++ }
func (v *CounterVec) Collect() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	lines := []string{fmt.Sprintf("# HELP %s %s", v.Name, v.Help), fmt.Sprintf("# TYPE %s counter", v.Name)}
	keys := sortedKeys(v.values)
	for _, key := range keys {
		cv := v.values[key]
		lines = append(lines, formatSample(v.Name, v.labelNames, cv.labels, cv.value))
	}
	return lines
}

type histogramValue struct {
	labels  []string
	count   uint64
	sum     float64
	buckets []uint64
}
type HistogramVec struct {
	mu sync.Mutex
	HistogramOpts
	labelNames []string
	values     map[string]*histogramValue
}
type Observer interface{ Observe(float64) }
type histogramHandle struct {
	vec *HistogramVec
	key string
}

func NewHistogramVec(opts HistogramOpts, labelNames []string) *HistogramVec {
	if len(opts.Buckets) == 0 {
		opts.Buckets = DefBuckets
	}
	return &HistogramVec{HistogramOpts: opts, labelNames: labelNames, values: map[string]*histogramValue{}}
}
func (v *HistogramVec) WithLabelValues(vals ...string) Observer {
	key := strings.Join(vals, "\xff")
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.values[key]; !ok {
		v.values[key] = &histogramValue{labels: append([]string(nil), vals...), buckets: make([]uint64, len(v.Buckets))}
	}
	return histogramHandle{vec: v, key: key}
}
func (h histogramHandle) Observe(val float64) {
	h.vec.mu.Lock()
	defer h.vec.mu.Unlock()
	hv := h.vec.values[h.key]
	hv.count++
	hv.sum += val
	for i, b := range h.vec.Buckets {
		if val <= b {
			hv.buckets[i]++
		}
	}
}
func (v *HistogramVec) Collect() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	lines := []string{fmt.Sprintf("# HELP %s %s", v.Name, v.Help), fmt.Sprintf("# TYPE %s histogram", v.Name)}
	keys := sortedKeys(v.values)
	for _, key := range keys {
		hv := v.values[key]
		cumulative := uint64(0)
		for i, b := range v.Buckets {
			cumulative += hv.buckets[i]
			lines = append(lines, formatSample(v.Name+"_bucket", append(v.labelNames, "le"), append(append([]string(nil), hv.labels...), fmt.Sprintf("%g", b)), float64(cumulative)))
		}
		lines = append(lines, formatSample(v.Name+"_bucket", append(v.labelNames, "le"), append(append([]string(nil), hv.labels...), "+Inf"), float64(hv.count)))
		lines = append(lines, formatSample(v.Name+"_sum", v.labelNames, hv.labels, hv.sum))
		lines = append(lines, formatSample(v.Name+"_count", v.labelNames, hv.labels, float64(hv.count)))
	}
	return lines
}

type Gauge interface {
	Collector
	Set(float64)
}
type gauge struct {
	mu sync.Mutex
	GaugeOpts
	value float64
}

func NewGauge(opts GaugeOpts) Gauge { return &gauge{GaugeOpts: opts} }
func (g *gauge) Set(v float64)      { g.mu.Lock(); defer g.mu.Unlock(); g.value = v }
func (g *gauge) Collect() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return []string{fmt.Sprintf("# HELP %s %s", g.Name, g.Help), fmt.Sprintf("# TYPE %s gauge", g.Name), fmt.Sprintf("%s %g", g.Name, g.value)}
}

func formatSample(name string, labelNames, labelValues []string, value float64) string {
	if len(labelNames) == 0 {
		return fmt.Sprintf("%s %g", name, value)
	}
	parts := make([]string, 0, len(labelNames))
	for i, n := range labelNames {
		parts = append(parts, fmt.Sprintf("%s=%q", n, labelValues[i]))
	}
	return fmt.Sprintf("%s{%s} %g", name, strings.Join(parts, ","), value)
}
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
