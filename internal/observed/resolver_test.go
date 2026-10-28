package observed

import (
	"context"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/store/memory"
)

func TestRegistryResolverResolvesRegisteredPusher(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	registryStore := registry.New(store)
	if err := registryStore.PutPusher(ctx, &registry.Pusher{
		Name:           "aws-1",
		ObservedAPIURL: "http://guardian-awsread:19090",
		Token:          "tok",
	}); err != nil {
		t.Fatalf("PutPusher() error = %v", err)
	}
	resolver := &RegistryResolver{Static: map[string]Endpoint{}, Registry: registryStore, TTL: time.Minute}
	endpoint, ok := resolver.Resolve("aws-1")
	if !ok {
		t.Fatalf("Resolve() not found")
	}
	if endpoint.Client.BaseURL != "http://guardian-awsread:19090" {
		t.Fatalf("BaseURL = %q", endpoint.Client.BaseURL)
	}
	if endpoint.Client.Token != "tok" {
		t.Fatalf("Token = %q", endpoint.Client.Token)
	}
	if _, ok := resolver.Resolve("missing"); ok {
		t.Fatalf("Resolve(missing) should fail")
	}
}

func TestRegistryResolverSkipsPushersWithoutObservedAPI(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	registryStore := registry.New(store)
	for _, pusher := range []registry.Pusher{
		{Name: "docker-main", Provider: "docker"},
		{Name: "k8s-main", Provider: "kubernetes"},
	} {
		if err := registryStore.PutPusher(ctx, &pusher); err != nil {
			t.Fatalf("PutPusher() error = %v", err)
		}
	}
	resolver := &RegistryResolver{Static: map[string]Endpoint{}, Registry: registryStore, TTL: time.Minute}
	for _, name := range []string{"docker-main", "k8s-main"} {
		if _, ok := resolver.Resolve(name); ok {
			t.Fatalf("Resolve(%s) should not return an empty endpoint", name)
		}
	}
}

func TestRegistryResolverStaticTakesPrecedence(t *testing.T) {
	static := Endpoint{Name: "aws-1", Client: NewClient("http://static:19090", "", time.Second)}
	resolver := &RegistryResolver{Static: map[string]Endpoint{"aws-1": static}}
	endpoint, ok := resolver.Resolve("aws-1")
	if !ok || endpoint.Client.BaseURL != "http://static:19090" {
		t.Fatalf("Resolve() = %+v, ok=%v", endpoint, ok)
	}
}

func TestRegistryResolverFallsBackToDirectReadOnCacheMiss(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	registryStore := registry.New(store)
	if err := registryStore.PutPusher(ctx, &registry.Pusher{
		Name:           "aws-new",
		ObservedAPIURL: "http://guardian-awsread:19090",
		Token:          "tok",
	}); err != nil {
		t.Fatalf("put pusher: %v", err)
	}

	resolver := &RegistryResolver{
		Registry:  registryStore,
		TTL:       time.Hour,
		cached:    map[string]Endpoint{},
		fetchedAt: time.Now().UTC(),
	}
	endpoint, ok := resolver.Resolve("aws-new")
	if !ok || endpoint.Client == nil || endpoint.Name != "aws-new" {
		t.Fatalf("resolve = %+v ok=%t, want direct-read endpoint", endpoint, ok)
	}
}
