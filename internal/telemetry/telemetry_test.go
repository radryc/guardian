package telemetry

import (
	"context"
	"testing"

	metricapi "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricViewsUseExponentialHistogram(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithView(metricViews([]string{"guardian.test.duration"})...),
	)
	histogram, err := provider.Meter("test").Float64Histogram("guardian.test.duration", metricapi.WithUnit("s"))
	if err != nil {
		t.Fatalf("Float64Histogram() error = %v", err)
	}
	histogram.Record(context.Background(), 0.25)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "guardian.test.duration" {
				continue
			}
			if _, ok := metric.Data.(metricdata.ExponentialHistogram[float64]); !ok {
				t.Fatalf("metric data = %T, want ExponentialHistogram", metric.Data)
			}
			return
		}
	}
	t.Fatal("guardian.test.duration was not collected")
}
