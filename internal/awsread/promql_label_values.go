package awsread

import (
	"context"
	"encoding/json"
	"sort"
)

// promqlLabelValuesFallback serves the Prometheus label-values API from the
// CloudWatch metric catalog where the native endpoint is unavailable.
func (h *Handler) promqlLabelValuesFallback(ctx context.Context, name string) []byte {
	if name != "__name__" {
		return nil
	}
	metrics, err := h.Reader.ListMetrics(ctx, "", "")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	for _, metric := range metrics {
		if metric.MetricName != "" {
			seen[metric.MetricName] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	payload, err := json.Marshal(map[string]any{"status": "success", "data": values})
	if err != nil {
		return nil
	}
	return payload
}
