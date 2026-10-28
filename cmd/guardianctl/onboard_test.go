package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestAWSOnboardCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/aws/onboarding":
			_ = json.NewEncoder(w).Encode(map[string]any{"descriptors": []any{map[string]any{
				"stack": "CustomerAppStack", "partition": "customer-prod-1", "intent": "app",
			}}})
		case strings.HasSuffix(r.URL.Path, "/template"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stackName": "CustomerAppStack",
				"template":  `{"Resources":{}}`,
			})
		case r.URL.Path == "/v1/aws/probes":
			_ = json.NewEncoder(w).Encode(map[string]any{"probes": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := memory.New()
	if err := registry.New(store).PutPusher(context.Background(), &registry.Pusher{
		Name: "aws-1", Account: "1", Region: "eu-west-1", ObservedAPIURL: server.URL,
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}
	var buf bytes.Buffer
	printer := &output.Printer{Format: format.FormatJSON, Writer: &buf}
	cmd := awsOnboardCommand(store, printer)
	if err := cmd.Flags.Parse([]string{"--pusher", "aws-1", "--reconcile=false"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if err := cmd.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(buf.String(), "customer-prod-1") {
		t.Fatalf("output = %s", buf.String())
	}
}
