package awsread

import "testing"

func TestNormalizeCloudWatchPromQL(t *testing.T) {
	cases := map[string]string{
		"RequestCount": `{"RequestCount"}`,
		`demo_build_requests_total{@aws.tag.guardian-partition="demo-build"}`: `{"demo_build_requests_total", @aws.tag.guardian-partition="demo-build"}`,
		`{"http.server.active_requests"}`:                                     `{"http.server.active_requests"}`,
		`sum(rate(RequestCount[5m]))`:                                         `sum(rate(RequestCount[5m]))`,
	}
	for in, want := range cases {
		if got := normalizeCloudWatchPromQL(in); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
