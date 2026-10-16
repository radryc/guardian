package monofs

import (
	"context"
	"fmt"

	pb "github.com/radryc/monofs/api/proto"
	"github.com/rydzu/ainfra/guardian/internal/workspacepub"
	"google.golang.org/grpc"
)

// WorkspacePublisher returns a publisher bound to the live router connection,
// so it survives router reconnects (the client re-dials the router on failure).
func (c *GRPCClient) WorkspacePublisher() *workspacepub.Publisher {
	return workspacepub.New(&liveRouter{client: c})
}

type liveRouter struct {
	client *GRPCClient
}

func (l *liveRouter) current() (routerClient, error) {
	if l == nil || l.client == nil {
		return nil, fmt.Errorf("monofs client is not configured")
	}
	router := l.client.currentRouter()
	if router == nil {
		return nil, fmt.Errorf("monofs router connection is not available")
	}
	return router, nil
}

func (l *liveRouter) UploadWorkspaceBundle(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.WorkspaceBundleChunk, pb.UploadWorkspaceBundleResponse], error) {
	router, err := l.current()
	if err != nil {
		return nil, err
	}
	return router.UploadWorkspaceBundle(ctx, opts...)
}

func (l *liveRouter) PublishWorkspace(ctx context.Context, in *pb.PublishWorkspaceRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WorkspaceSyncEvent], error) {
	router, err := l.current()
	if err != nil {
		return nil, err
	}
	return router.PublishWorkspace(ctx, in, opts...)
}

func (c *GRPCClient) currentRouter() routerClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.router
}
