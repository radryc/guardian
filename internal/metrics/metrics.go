// Package metrics provides the small Prometheus-shaped API Guardian code was
// written against, backed by OpenTelemetry instruments. Guardian exports
// metrics through its OTLP telemetry pipeline (Doctor's metric store), not the
// Prometheus scrape endpoint.
package metrics

import (
	"context"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metricapi "go.opentelemetry.io/otel/metric"
)

const scope = "guardian.metrics"

// CounterVec is a monotonically increasing counter partitioned by labels.
type CounterVec struct {
	name   string
	help   string
	labels []string

	once sync.Once
	inst metricapi.Int64Counter
}

func NewCounter(name, help string, labels ...string) *CounterVec {
	return &CounterVec{name: name, help: help, labels: labels}
}

type Counter struct {
	inst  metricapi.Int64Counter
	attrs []attribute.KeyValue
}

func (c *CounterVec) init() {
	c.once.Do(func() {
		inst, err := otel.Meter(scope).Int64Counter(c.name, metricapi.WithDescription(c.help))
		if err == nil {
			c.inst = inst
		}
	})
}

func (c *CounterVec) WithLabelValues(values ...string) Counter {
	c.init()
	return Counter{inst: c.inst, attrs: attributes(c.labels, values)}
}

// Inc and Add are convenience methods for counters without labels.
func (c *CounterVec) Inc() {
	c.WithLabelValues().Inc()
}

func (c *CounterVec) Add(value float64) {
	c.WithLabelValues().Add(value)
}

func (c Counter) Inc() { c.Add(1) }

func (c Counter) Add(value float64) {
	if c.inst == nil {
		return
	}
	c.inst.Add(context.Background(), int64(value), metricapi.WithAttributes(c.attrs...))
}

// GaugeVec is a snapshot gauge partitioned by labels. Values are reported by an
// observable callback so Reset and label deletion behave like the Prometheus
// gauges this package replaces.
type GaugeVec struct {
	name   string
	help   string
	labels []string

	initOnce sync.Once
	mu       sync.Mutex
	series   map[string]gaugeSeries
}

type gaugeSeries struct {
	attrs []attribute.KeyValue
	value float64
}

func NewGauge(name, help string, labels ...string) *GaugeVec {
	g := &GaugeVec{name: name, help: help, labels: labels}
	gaugeRegistryMu.Lock()
	gaugeRegistry[name] = g
	gaugeRegistryMu.Unlock()
	return g
}

// Sample is one gauge series as exposed for tests and diagnostics.
type Sample struct {
	Labels map[string]string
	Value  float64
}

var (
	gaugeRegistryMu sync.Mutex
	gaugeRegistry   = map[string]*GaugeVec{}
)

// Snapshot returns the current series of a registered gauge. It exists so unit
// tests can assert metric state without standing up a full OTel pipeline.
func Snapshot(name string) []Sample {
	gaugeRegistryMu.Lock()
	g := gaugeRegistry[name]
	gaugeRegistryMu.Unlock()
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Sample, 0, len(g.series))
	for key, series := range g.series {
		values := strings.Split(key, "\x00")
		labels := make(map[string]string, len(g.labels))
		for i, label := range g.labels {
			if i < len(values) {
				labels[label] = values[i]
			}
		}
		out = append(out, Sample{Labels: labels, Value: series.value})
	}
	return out
}

type Gauge struct {
	vec   *GaugeVec
	key   string
	attrs []attribute.KeyValue
}

func (g *GaugeVec) init() {
	g.initOnce.Do(func() {
		meter := otel.Meter(scope)
		inst, err := meter.Float64ObservableGauge(g.name, metricapi.WithDescription(g.help))
		if err != nil {
			return
		}
		_, _ = meter.RegisterCallback(func(_ context.Context, observer metricapi.Observer) error {
			g.mu.Lock()
			defer g.mu.Unlock()
			for _, series := range g.series {
				observer.ObserveFloat64(inst, series.value, metricapi.WithAttributes(series.attrs...))
			}
			return nil
		}, inst)
	})
}

func (g *GaugeVec) WithLabelValues(values ...string) *Gauge {
	g.init()
	return &Gauge{vec: g, key: strings.Join(values, "\x00"), attrs: attributes(g.labels, values)}
}

func (g *Gauge) Set(value float64) {
	g.vec.mu.Lock()
	if g.vec.series == nil {
		g.vec.series = map[string]gaugeSeries{}
	}
	g.vec.series[g.key] = gaugeSeries{attrs: g.attrs, value: value}
	g.vec.mu.Unlock()
}

func (g *Gauge) Add(delta float64) {
	g.vec.mu.Lock()
	if g.vec.series == nil {
		g.vec.series = map[string]gaugeSeries{}
	}
	series := g.vec.series[g.key]
	series.attrs = g.attrs
	series.value += delta
	g.vec.series[g.key] = series
	g.vec.mu.Unlock()
}

func (g *Gauge) Inc() { g.Add(1) }
func (g *Gauge) Dec() { g.Add(-1) }

func (g *GaugeVec) DeleteLabelValues(values ...string) {
	key := strings.Join(values, "\x00")
	g.mu.Lock()
	delete(g.series, key)
	g.mu.Unlock()
}

func (g *GaugeVec) Reset() {
	g.mu.Lock()
	g.series = map[string]gaugeSeries{}
	g.mu.Unlock()
}

// attributes maps Prometheus-style label names to OpenTelemetry attributes.
// Guardian labels use the dotted spelling Doctor expects; plain labels such as
// "status" and "operation" stay unchanged.
func attributes(labels, values []string) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(labels))
	for i, label := range labels {
		if i >= len(values) {
			break
		}
		if strings.HasPrefix(label, "guardian_") {
			label = strings.ReplaceAll(label, "_", ".")
		}
		out = append(out, attribute.String(label, values[i]))
	}
	return out
}
