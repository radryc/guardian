package secrets

import (
	"context"
	"testing"

	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestStoreResolverResolveMonofsSecret(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: "/partitions/shared/secrets/encryption-key",
			Content:     []byte("abc123\n"),
		}},
		Context: guardianapi.MutationContext{PrincipalID: "test", Reason: "seed secret"},
	}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	value, err := NewStoreResolver(store).Resolve(ctx, "monofs-secret://shared/encryption-key")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if value != "abc123" {
		t.Fatalf("Resolve() = %q, want %q", value, "abc123")
	}
}

func TestStoreResolverRejectsTraversal(t *testing.T) {
	_, err := secretLogicalPath("monofs-secret://shared/../encryption-key")
	if err == nil {
		t.Fatalf("expected traversal secret ref to fail")
	}
}
