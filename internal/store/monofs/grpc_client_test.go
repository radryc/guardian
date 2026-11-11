package monofs

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/radryc/monofs/api/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/radryc/guardian/pkg/guardianapi"
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

func TestRefreshNodesReplacesConnectionWhenAddressChanges(t *testing.T) {
	t.Parallel()

	router := &fakeRouterClient{clusterInfoResp: &pb.ClusterInfoResponse{Nodes: []*pb.NodeInfo{{
		NodeId:  "node-a",
		Address: "127.0.0.1:19001",
		Healthy: true,
	}}}}
	client := &GRPCClient{
		rpcTimeout:  time.Second,
		router:      router,
		nodeConns:   map[string]*grpc.ClientConn{},
		nodeClients: map[string]pb.MonoFSClient{},
		nodeAddrs:   map[string]string{},
	}

	if err := client.refreshNodes(context.Background()); err != nil {
		t.Fatalf("refreshNodes() first call error = %v", err)
	}
	firstConn := client.nodeConns["node-a"]
	if firstConn == nil {
		t.Fatal("expected first connection for node-a")
	}

	router.clusterInfoResp = &pb.ClusterInfoResponse{Nodes: []*pb.NodeInfo{{
		NodeId:  "node-a",
		Address: "127.0.0.1:19002",
		Healthy: true,
	}}}

	if err := client.refreshNodes(context.Background()); err != nil {
		t.Fatalf("refreshNodes() second call error = %v", err)
	}
	secondConn := client.nodeConns["node-a"]
	if secondConn == nil {
		t.Fatal("expected second connection for node-a")
	}
	if firstConn == secondConn {
		t.Fatal("expected node-a connection to be replaced when address changes")
	}
	if got := client.nodeAddrs["node-a"]; got != "127.0.0.1:19002" {
		t.Fatalf("nodeAddrs[node-a] = %q, want %q", got, "127.0.0.1:19002")
	}
}

func TestInvalidateNodeOnTransportErrorQuarantinesNode(t *testing.T) {
	t.Parallel()

	client := &GRPCClient{
		nodeConns:   map[string]*grpc.ClientConn{"node-a": nil},
		nodeClients: map[string]pb.MonoFSClient{"node-a": nil},
		nodeAddrs:   map[string]string{"node-a": "127.0.0.1:19001"},
		lastRefresh: time.Now(),
	}

	client.invalidateNodeOnTransportError("node-a", status.Error(codes.Unavailable, "error reading server preface: EOF"))

	if _, ok := client.nodeClients["node-a"]; !ok {
		t.Fatal("expected node client cache to be preserved for gRPC reconnect")
	}
	if _, ok := client.nodeAddrs["node-a"]; !ok {
		t.Fatal("expected node address cache to be preserved")
	}
	until, ok := client.nodeFailedUntil["node-a"]
	if !ok || !until.After(time.Now()) {
		t.Fatal("expected node-a to be in cooldown")
	}
	if client.lastRefresh.IsZero() {
		t.Fatal("expected topology cache timestamp to be preserved")
	}
}

func TestPreferAvailableNodesSkipsCoolingNode(t *testing.T) {
	t.Parallel()

	client := &GRPCClient{
		nodeFailedUntil: map[string]time.Time{
			"node-a": time.Now().Add(time.Minute),
		},
	}
	nodes := []nodeTarget{{id: "node-a"}, {id: "node-b"}}

	got := client.preferAvailableNodes(nodes)
	if len(got) != 1 || got[0].id != "node-b" {
		t.Fatalf("preferAvailableNodes() = %+v, want only node-b", got)
	}

	client.nodeFailedUntil["node-b"] = time.Now().Add(time.Minute)
	got = client.preferAvailableNodes(nodes)
	if len(got) != 2 {
		t.Fatalf("preferAvailableNodes() with all nodes cooling = %d nodes, want fallback to both", len(got))
	}
}

func TestIsTransportUnavailable(t *testing.T) {
	t.Parallel()

	if !isTransportUnavailable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("expected grpc unavailable to be treated as transport unavailable")
	}
	if !isTransportUnavailable(errors.New("error reading server preface: read tcp ...")) {
		t.Fatal("expected preface read error to be treated as transport unavailable")
	}
	if !isTransportUnavailable(status.Error(codes.Internal, "upsert guardian paths: node node-a ingest guardian/x: ... dial tcp 10.96.0.1:9000: connect: connection refused")) {
		t.Fatal("expected node connection refused wrapped as Internal to be retryable")
	}
	if isTransportUnavailable(status.Error(codes.InvalidArgument, "bad request")) {
		t.Fatal("did not expect invalid argument to be treated as transport unavailable")
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

func (f *fakeRouterClient) UpsertGuardianPathsStream(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[pb.GuardianPathWriteChunk, pb.UpsertGuardianPathsResponse], error) {
	return nil, nil
}

func (f *fakeRouterClient) UploadWorkspaceBundle(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[pb.WorkspaceBundleChunk, pb.UploadWorkspaceBundleResponse], error) {
	return nil, nil
}

func (f *fakeRouterClient) PublishWorkspace(context.Context, *pb.PublishWorkspaceRequest, ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WorkspaceSyncEvent], error) {
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

func (f *fakeRouterClient) ReadGuardianPath(context.Context, *pb.ReadGuardianPathRequest, ...grpc.CallOption) (*pb.ReadGuardianPathResponse, error) {
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
	cloned := proto.Clone(req).(*pb.RegisterClientRequest)
	if req.GuardianConfig != nil {
		cfg := proto.Clone(req.GuardianConfig).(*pb.GuardianConfig)
		cloned.GuardianConfig = cfg
	}
	f.registerReqs = append(f.registerReqs, cloned)
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
	cloned := proto.Clone(req).(*pb.ClientHeartbeatRequest)
	f.heartbeatReqs = append(f.heartbeatReqs, cloned)
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

// startupRetryRouter simulates a router that is still rolling: the first
// GetClusterInfo calls fail with the same transport error seen when a router
// pod is replaced, then it starts serving normally.
type startupRetryRouter struct {
	pb.UnimplementedMonoFSRouterServer

	mu            sync.Mutex
	failures      int
	clusterCalls  int
	registerCalls int
}

func (r *startupRetryRouter) GetClusterInfo(context.Context, *pb.ClusterInfoRequest) (*pb.ClusterInfoResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clusterCalls++
	if r.clusterCalls <= r.failures {
		return nil, status.Error(codes.Unavailable, "error reading server preface: EOF")
	}
	return &pb.ClusterInfoResponse{Nodes: []*pb.NodeInfo{{
		NodeId:  "node-a",
		Address: "127.0.0.1:1",
		Healthy: true,
	}}}, nil
}

func (r *startupRetryRouter) RegisterClient(context.Context, *pb.RegisterClientRequest) (*pb.RegisterClientResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerCalls++
	return &pb.RegisterClientResponse{Success: true, HeartbeatIntervalMs: 30000}, nil
}

func TestNewGRPCClientRetriesTransientRouterStartup(t *testing.T) {
	oldBase := initRetryBase
	initRetryBase = 10 * time.Millisecond
	t.Cleanup(func() { initRetryBase = oldBase })

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	router := &startupRetryRouter{failures: 2}
	pb.RegisterMonoFSRouterServer(server, router)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	client, err := NewGRPCClient(context.Background(), ClientConfig{
		RouterAddr: lis.Addr().String(),
		ClientID:   "guardianctl-test",
		Token:      "test-token",
		RPCTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewGRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	router.mu.Lock()
	clusterCalls, registerCalls := router.clusterCalls, router.registerCalls
	router.mu.Unlock()
	if clusterCalls != 3 {
		t.Fatalf("GetClusterInfo call count = %d, want 3 (two transient EOFs then success)", clusterCalls)
	}
	if registerCalls != 1 {
		t.Fatalf("RegisterClient call count = %d, want 1", registerCalls)
	}
}

func TestMapRPCErrorTreatsVersionMismatchAsConflict(t *testing.T) {
	t.Parallel()

	mismatch := status.Error(codes.Unavailable,
		`guardian content for "/partitions/demo/.state/intents/app.json" does not match version ver_1`)
	if !errors.Is(mapRPCError(mismatch), guardianapi.ErrConflict) {
		t.Fatalf("mapRPCError(version mismatch) = %v, want ErrConflict", mapRPCError(mismatch))
	}

	transport := status.Error(codes.Unavailable, "connection refused")
	if errors.Is(mapRPCError(transport), guardianapi.ErrConflict) {
		t.Fatalf("mapRPCError(transport) = %v, should not be ErrConflict", mapRPCError(transport))
	}
}

func TestShouldInstrumentMonoFSRPC(t *testing.T) {
	t.Parallel()

	if shouldInstrumentMonoFSRPC(nil) != true {
		t.Fatal("nil RPCTagInfo should be instrumented")
	}
	if shouldInstrumentMonoFSRPC(&stats.RPCTagInfo{FullMethodName: pb.MonoFSRouter_SubscribeGuardianChanges_FullMethodName}) {
		t.Fatal("long-lived guardian change subscription must not be instrumented")
	}
	if !shouldInstrumentMonoFSRPC(&stats.RPCTagInfo{FullMethodName: pb.MonoFS_Read_FullMethodName}) {
		t.Fatal("unary MonoFS read should be instrumented")
	}
}
