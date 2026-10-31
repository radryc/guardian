package main

import "testing"

func TestIsLoopbackListen(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:19090", "localhost:19090", "[::1]:19090"} {
		if !isLoopbackListen(addr) {
			t.Fatalf("isLoopbackListen(%q) = false, want true", addr)
		}
	}
	for _, addr := range []string{":19090", "0.0.0.0:19090", "10.0.0.5:19090", "[::]:19090"} {
		if isLoopbackListen(addr) {
			t.Fatalf("isLoopbackListen(%q) = true, want false", addr)
		}
	}
}
