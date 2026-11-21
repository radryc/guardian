package main

import (
	"slices"
	"testing"
)

func TestStoreFlagsNeverIncludeToken(t *testing.T) {
	args := storeFlags("monofs.example.com:9090")
	if !slices.Contains(args, "--monofs-router") {
		t.Fatalf("store flags = %v, want --monofs-router", args)
	}
	if slices.Contains(args, "--monofs-token") || slices.Contains(args, defaultLocalMonoFSToken) {
		t.Fatalf("store flags must not disclose the token: %v", args)
	}
}

func TestStoreFlagsOmitEmptyRouter(t *testing.T) {
	if args := storeFlags(""); len(args) != 0 {
		t.Fatalf("store flags = %v, want empty", args)
	}
}
