package main

import (
	"context"

	"github.com/rydzu/ainfra/guardian/internal/unmanaged"
	"github.com/rydzu/ainfra/guardian/internal/workspacepub"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// workspacePublisherFromStore returns a lazy MonoFS workspace publisher when
// the store is MonoFS-backed, so the unmanaged association flow can publish
// review branches. It returns nil for filesystem stores.
func workspacePublisherFromStore(store guardianapi.Store) func(context.Context) unmanaged.Publisher {
	return func(ctx context.Context) unmanaged.Publisher {
		lazy, ok := store.(*lazyStore)
		if !ok {
			return nil
		}
		if _, err := lazy.resolve(ctx); err != nil {
			return nil
		}
		lazy.mu.Lock()
		closer := lazy.closer
		lazy.mu.Unlock()
		provider, ok := closer.(interface {
			WorkspacePublisher() *workspacepub.Publisher
		})
		if !ok || provider == nil {
			return nil
		}
		return provider.WorkspacePublisher()
	}
}
