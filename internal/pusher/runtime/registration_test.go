package runtime

import (
	"context"
	"errors"
	"os"
	"testing"

	platformregistry "github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestStartRegistrationPreservesRegistrationURLAndToken(t *testing.T) {
	store := memory.New()
	reg := platformregistry.New(store)
	ctx := context.Background()
	if err := reg.PutPusher(ctx, &platformregistry.Pusher{
		Name:           "aws-test",
		Provider:       "aws",
		Account:        "111111111111",
		Region:         "us-east-1",
		ObservedAPIURL: "http://172.18.0.1:19290",
		Token:          "pusher-token",
		Description:    "demo pusher",
		Labels:         map[string]string{"owner": "demo"},
	}); err != nil {
		t.Fatalf("seed pusher: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	StartRegistration(runCtx, store, PusherInfo{
		Name:        "aws-test",
		Provider:    "aws",
		Account:     "111111111111",
		Region:      "us-east-1",
		WorkerID:    "worker-1",
		ShardCount:  1,
		Description: "",
	})

	got, err := reg.GetPusher(ctx, "aws-test")
	if err != nil {
		t.Fatalf("GetPusher() error = %v", err)
	}
	if got.ObservedAPIURL != "http://172.18.0.1:19290" {
		t.Fatalf("observedApiUrl = %q", got.ObservedAPIURL)
	}
	if got.Token != "pusher-token" {
		t.Fatalf("token = %q", got.Token)
	}
	if got.Labels["owner"] != "demo" || got.Labels["workerID"] != "worker-1" {
		t.Fatalf("labels = %#v", got.Labels)
	}
	if got.Description != "demo pusher" {
		t.Fatalf("description = %q", got.Description)
	}
}

func TestStartRegistrationWaitsForRegistrationWithoutURL(t *testing.T) {
	store := memory.New()
	reg := platformregistry.New(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartRegistration(ctx, store, PusherInfo{Name: "aws-unregistered", Provider: "aws", Account: "111"})
	if _, err := reg.GetPusher(context.Background(), "aws-unregistered"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GetPusher() error = %v, want not found", err)
	}
}
