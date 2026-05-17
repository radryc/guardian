package monofs

import (
	"context"
	"io"
	"testing"
	"time"

	pb "github.com/radryc/monofs/api/proto"
	"google.golang.org/grpc"
)

func TestRegisterUsesRouterSuccessResponse(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{
		registerResp: &pb.RegisterClientResponse{
			Success:             true,
			HeartbeatIntervalMs: 45000,
		},
	}
	client := &GRPCClient{
		clientID:    "guardian-control-plane-1",
		token:       "secret-token",
		principalID: "guardian",
		role:        "control-plane",
		baseURL:     "http://guardian.example/ui",
		version:     "guardiand",
		hostname:    "guardian-host",
		writable:    true,
		rpcTimeout:  time.Second,
		router:      router,
	}

	interval, err := client.register(context.Background())
	if err != nil {
		t.Fatalf("register() error = %v", err)
	}
	if interval != 45*time.Second {
		t.Fatalf("register() interval = %v, want %v", interval, 45*time.Second)
	}
	if len(router.registerReqs) != 1 {
		t.Fatalf("register call count = %d, want 1", len(router.registerReqs))
	}

	req := router.registerReqs[0]
	if req.GetClientId() != "guardian-control-plane-1" {
		t.Fatalf("client_id = %q", req.GetClientId())
	}
	cfg := req.GetGuardianConfig()
	if cfg == nil {
		t.Fatal("expected guardian_config")
	}
	if cfg.GetAuthToken() != "secret-token" {
		t.Fatalf("auth_token = %q", cfg.GetAuthToken())
	}
	if cfg.GetPrincipalId() != "guardian" {
		t.Fatalf("principal_id = %q", cfg.GetPrincipalId())
	}
	if cfg.GetRole() != "control-plane" {
		t.Fatalf("role = %q", cfg.GetRole())
	}
	if cfg.GetBaseUrl() != "http://guardian.example/ui" {
		t.Fatalf("base_url = %q", cfg.GetBaseUrl())
	}
}

func TestRegisterRejectsUnsuccessfulResponse(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{
		registerResp: &pb.RegisterClientResponse{
			Success: false,
			Message: "guardian_config with auth_token required for guardian-* clients",
		},
	}
	client := &GRPCClient{
		clientID:   "guardian-cli-1",
		token:      "secret-token",
		rpcTimeout: time.Second,
		router:     router,
	}

	if _, err := client.register(context.Background()); err == nil {
		t.Fatal("expected register() to fail when router rejects the client")
	}
}

func TestSendHeartbeatReRegistersWhenRequested(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{
		registerResp: &pb.RegisterClientResponse{
			Success:             true,
			HeartbeatIntervalMs: 30000,
		},
		heartbeatResp: &pb.ClientHeartbeatResponse{
			Success:        false,
			ShouldRegister: true,
		},
	}
	client := &GRPCClient{
		clientID:    "guardian-pusher-local-1",
		token:       "secret-token",
		principalID: "guardian-pusher",
		role:        "pusher",
		rpcTimeout:  time.Second,
		router:      router,
	}

	client.sendHeartbeat()

	if len(router.heartbeatReqs) != 1 {
		t.Fatalf("heartbeat call count = %d, want 1", len(router.heartbeatReqs))
	}
	if len(router.registerReqs) != 1 {
		t.Fatalf("re-register call count = %d, want 1", len(router.registerReqs))
	}
}

func TestHealthyNodesFallsBackToCachedNodesWhenClusterRefreshFails(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{clusterInfoErr: context.DeadlineExceeded}
	client := &GRPCClient{
		rpcTimeout: time.Second,
		router:     router,
		nodeClients: map[string]pb.MonoFSClient{
			"node-a": nil,
		},
	}

	nodes, err := client.healthyNodes(context.Background())
	if err != nil {
		t.Fatalf("healthyNodes() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("healthyNodes() len = %d, want 1", len(nodes))
	}
	if nodes[0].id != "node-a" {
		t.Fatalf("healthyNodes()[0].id = %q, want %q", nodes[0].id, "node-a")
	}
}

func TestHealthyNodesPreservesCachedNodesWhenRouterReportsNone(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{clusterInfoResp: &pb.ClusterInfoResponse{}}
	client := &GRPCClient{
		rpcTimeout: time.Second,
		router:     router,
		nodeClients: map[string]pb.MonoFSClient{
			"node-a": nil,
		},
	}

	nodes, err := client.healthyNodes(context.Background())
	if err != nil {
		t.Fatalf("healthyNodes() error = %v, want cached nodes fallback", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("healthyNodes() len = %d, want 1", len(nodes))
	}
	if nodes[0].id != "node-a" {
		t.Fatalf("healthyNodes()[0].id = %q, want %q", nodes[0].id, "node-a")
	}
}

func TestHealthyNodesUsesFreshCacheWithoutRefreshingCluster(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{clusterInfoErr: context.DeadlineExceeded}
	client := &GRPCClient{
		rpcTimeout: time.Second,
		router:     router,
		nodeClients: map[string]pb.MonoFSClient{
			"node-a": nil,
		},
		lastRefresh: time.Now(),
		refreshTTL:  time.Minute,
	}

	nodes, err := client.healthyNodes(context.Background())
	if err != nil {
		t.Fatalf("healthyNodes() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("healthyNodes() len = %d, want 1", len(nodes))
	}
	if router.clusterInfoCalls != 0 {
		t.Fatalf("GetClusterInfo call count = %d, want 0", router.clusterInfoCalls)
	}
}

func TestWatchUsesLogicalPrefixes(t *testing.T) {
	t.Parallel()

	stream := &fakeGuardianChangeStream{
		events: []*pb.GuardianChangeEvent{{
			LogicalPath: "/partitions/shared/config.yaml",
		}},
	}
	router := &fakeRouterClient{watchStream: stream}
	client := &GRPCClient{
		token:      "secret-token",
		rpcTimeout: time.Second,
		router:     router,
	}

	ch, err := client.Watch(context.Background(), []string{"/partitions/shared"})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	event := <-ch
	if got := event.LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("event.LogicalPath = %q, want logical path", got)
	}
	if len(router.watchReqs) != 1 {
		t.Fatalf("watch call count = %d, want 1", len(router.watchReqs))
	}
	if got := router.watchReqs[0].GetLogicalPrefixes(); len(got) != 1 || got[0] != "/partitions/shared" {
		t.Fatalf("LogicalPrefixes = %v, want logical prefixes", got)
	}
}

type fakeRouterClient struct {
	registerResp    *pb.RegisterClientResponse
	registerErr     error
	clusterInfoResp *pb.ClusterInfoResponse
	clusterInfoErr  error
	heartbeatResp   *pb.ClientHeartbeatResponse
	heartbeatErr    error
	watchStream     grpc.ServerStreamingClient[pb.GuardianChangeEvent]

	registerReqs     []*pb.RegisterClientRequest
	heartbeatReqs    []*pb.ClientHeartbeatRequest
	watchReqs        []*pb.SubscribeGuardianChangesRequest
	clusterInfoCalls int
}

func (f *fakeRouterClient) UpsertGuardianPaths(context.Context, *pb.UpsertGuardianPathsRequest, ...grpc.CallOption) (*pb.UpsertGuardianPathsResponse, error) {
	return nil, nil
}

func (f *fakeRouterClient) DeleteGuardianPaths(context.Context, *pb.DeleteGuardianPathsRequest, ...grpc.CallOption) (*pb.DeleteGuardianPathsResponse, error) {
	return nil, nil
}

func (f *fakeRouterClient) ListGuardianVersions(context.Context, *pb.ListGuardianVersionsRequest, ...grpc.CallOption) (*pb.ListGuardianVersionsResponse, error) {
	return nil, nil
}

func (f *fakeRouterClient) GetGuardianVersion(context.Context, *pb.GetGuardianVersionRequest, ...grpc.CallOption) (*pb.GetGuardianVersionResponse, error) {
	return nil, nil
}

func (f *fakeRouterClient) SubscribeGuardianChanges(_ context.Context, req *pb.SubscribeGuardianChangesRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.GuardianChangeEvent], error) {
	cloned := &pb.SubscribeGuardianChangesRequest{
		GuardianToken:        req.GetGuardianToken(),
		LogicalPrefixes:      append([]string(nil), req.GetLogicalPrefixes()...),
		IncludeInlineContent: req.GetIncludeInlineContent(),
	}
	f.watchReqs = append(f.watchReqs, cloned)
	if f.watchStream != nil {
		return f.watchStream, nil
	}
	return &fakeGuardianChangeStream{}, nil
}

func (f *fakeRouterClient) RegisterClient(_ context.Context, req *pb.RegisterClientRequest, _ ...grpc.CallOption) (*pb.RegisterClientResponse, error) {
	cloned := *req
	if req.GuardianConfig != nil {
		cfg := *req.GuardianConfig
		cloned.GuardianConfig = &cfg
	}
	f.registerReqs = append(f.registerReqs, &cloned)
	if f.registerResp != nil || f.registerErr != nil {
		return f.registerResp, f.registerErr
	}
	return &pb.RegisterClientResponse{Success: true, HeartbeatIntervalMs: 30000}, nil
}

func (f *fakeRouterClient) UnregisterClient(context.Context, *pb.UnregisterClientRequest, ...grpc.CallOption) (*pb.UnregisterClientResponse, error) {
	return &pb.UnregisterClientResponse{Success: true}, nil
}

func (f *fakeRouterClient) GetClusterInfo(context.Context, *pb.ClusterInfoRequest, ...grpc.CallOption) (*pb.ClusterInfoResponse, error) {
	f.clusterInfoCalls++
	if f.clusterInfoResp != nil || f.clusterInfoErr != nil {
		return f.clusterInfoResp, f.clusterInfoErr
	}
	return &pb.ClusterInfoResponse{}, nil
}

func (f *fakeRouterClient) ClientHeartbeat(_ context.Context, req *pb.ClientHeartbeatRequest, _ ...grpc.CallOption) (*pb.ClientHeartbeatResponse, error) {
	cloned := *req
	f.heartbeatReqs = append(f.heartbeatReqs, &cloned)
	if f.heartbeatResp != nil || f.heartbeatErr != nil {
		return f.heartbeatResp, f.heartbeatErr
	}
	return &pb.ClientHeartbeatResponse{Success: true}, nil
}

type fakeGuardianChangeStream struct {
	grpc.ServerStreamingClient[pb.GuardianChangeEvent]
	events []*pb.GuardianChangeEvent
	index  int
}

func (f *fakeGuardianChangeStream) Recv() (*pb.GuardianChangeEvent, error) {
	if f.index >= len(f.events) {
		return nil, io.EOF
	}
	event := f.events[f.index]
	f.index++
	return event, nil
}

var _ routerClient = (*fakeRouterClient)(nil)
