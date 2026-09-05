package monofs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	pb "github.com/radryc/monofs/api/proto"
	"github.com/rydzu/ainfra/guardian/internal/clitoken"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

const defaultClientHeartbeatInterval = 30 * time.Second
const defaultTopologyRefreshInterval = 5 * time.Second

// cliBearerCreds attaches the cached `guardianctl login` token (if any) as an
// Authorization: Bearer header on outgoing RPCs, so human CLI calls carry an
// SSO identity. It is a no-op when no token is cached (machines/services keep
// using their own token mechanism).
type cliBearerCreds struct{}

func (cliBearerCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	if tok := clitoken.Bearer(); tok != "" {
		return map[string]string{"authorization": "Bearer " + tok}, nil
	}
	return nil, nil
}

func (cliBearerCreds) RequireTransportSecurity() bool { return false }

type routerClient interface {
	UpsertGuardianPaths(ctx context.Context, in *pb.UpsertGuardianPathsRequest, opts ...grpc.CallOption) (*pb.UpsertGuardianPathsResponse, error)
	UpsertGuardianPathsStream(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.GuardianPathWriteChunk, pb.UpsertGuardianPathsResponse], error)
	DeleteGuardianPaths(ctx context.Context, in *pb.DeleteGuardianPathsRequest, opts ...grpc.CallOption) (*pb.DeleteGuardianPathsResponse, error)
	ListGuardianVersions(ctx context.Context, in *pb.ListGuardianVersionsRequest, opts ...grpc.CallOption) (*pb.ListGuardianVersionsResponse, error)
	GetGuardianVersion(ctx context.Context, in *pb.GetGuardianVersionRequest, opts ...grpc.CallOption) (*pb.GetGuardianVersionResponse, error)
	SubscribeGuardianChanges(ctx context.Context, in *pb.SubscribeGuardianChangesRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[pb.GuardianChangeEvent], error)
	RegisterClient(ctx context.Context, in *pb.RegisterClientRequest, opts ...grpc.CallOption) (*pb.RegisterClientResponse, error)
	UnregisterClient(ctx context.Context, in *pb.UnregisterClientRequest, opts ...grpc.CallOption) (*pb.UnregisterClientResponse, error)
	GetClusterInfo(ctx context.Context, in *pb.ClusterInfoRequest, opts ...grpc.CallOption) (*pb.ClusterInfoResponse, error)
	ClientHeartbeat(ctx context.Context, in *pb.ClientHeartbeatRequest, opts ...grpc.CallOption) (*pb.ClientHeartbeatResponse, error)
}

type ClientConfig struct {
	RouterAddr           string
	ClientID             string
	Token                string
	PrincipalID          string
	Role                 string
	BaseURL              string
	Version              string
	Hostname             string
	MountPoint           string
	UseExternalAddresses bool
	RPCTimeout           time.Duration
	Writable             bool
}

type GRPCClient struct {
	routerAddr           string
	routerHost           string
	clientID             string
	token                string
	principalID          string
	role                 string
	baseURL              string
	version              string
	hostname             string
	mountPoint           string
	useExternalAddresses bool
	rpcTimeout           time.Duration
	writable             bool

	mu       sync.Mutex
	routerMu sync.Mutex

	routerConn  *grpc.ClientConn
	router      routerClient
	nodeConns   map[string]*grpc.ClientConn
	nodeClients map[string]pb.MonoFSClient
	nodeAddrs   map[string]string
	lastRefresh time.Time
	refreshTTL  time.Duration

	refreshMu      sync.Mutex
	refreshing     bool
	refreshRequest chan struct{}

	stopHeartbeat chan struct{}
	stopOnce      sync.Once
	heartbeatWG   sync.WaitGroup
}

type nodeTarget struct {
	id     string
	client pb.MonoFSClient
}

func dialRouter(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(cliBearerCreds{}),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1024*1024*1024),
			grpc.MaxCallSendMsgSize(1024*1024*1024),
		),
	)
}

func NewGRPCClient(ctx context.Context, cfg ClientConfig) (*GRPCClient, error) {
	if cfg.RouterAddr == "" {
		return nil, fmt.Errorf("monofs router address is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("monofs client id is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("monofs guardian token is required")
	}
	if cfg.PrincipalID == "" {
		cfg.PrincipalID = cfg.ClientID
	}
	if cfg.Role == "" {
		cfg.Role = "control-plane"
	}
	if cfg.Version == "" {
		cfg.Version = "guardian"
	}
	if cfg.RPCTimeout <= 0 {
		cfg.RPCTimeout = 10 * time.Second
	}
	if cfg.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("resolve hostname: %w", err)
		}
		cfg.Hostname = hostname
	}

	conn, err := dialRouter(cfg.RouterAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to monofs router: %w", err)
	}

	routerHost, _, err := net.SplitHostPort(cfg.RouterAddr)
	if err != nil {
		return nil, fmt.Errorf("parse monofs router address %q: %w", cfg.RouterAddr, err)
	}

	client := &GRPCClient{
		routerAddr:           cfg.RouterAddr,
		routerHost:           routerHost,
		clientID:             cfg.ClientID,
		token:                cfg.Token,
		principalID:          cfg.PrincipalID,
		role:                 cfg.Role,
		baseURL:              strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		version:              cfg.Version,
		hostname:             cfg.Hostname,
		mountPoint:           cfg.MountPoint,
		useExternalAddresses: cfg.UseExternalAddresses,
		rpcTimeout:           cfg.RPCTimeout,
		writable:             cfg.Writable,
		routerConn:           conn,
		router:               pb.NewMonoFSRouterClient(conn),
		nodeConns:            map[string]*grpc.ClientConn{},
		nodeClients:          map[string]pb.MonoFSClient{},
		nodeAddrs:            map[string]string{},
		refreshTTL:           defaultTopologyRefreshInterval,
		stopHeartbeat:        make(chan struct{}),
	}

	if err := client.refreshNodes(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	heartbeatInterval, err := client.register(ctx)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	client.startHeartbeatLoop(heartbeatInterval)
	return client, nil
}

func (c *GRPCClient) Close() error {
	c.stopHeartbeatLoop()

	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	if c.router != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.router.UnregisterClient(ctx, &pb.UnregisterClientRequest{
			ClientId: c.clientID,
			Reason:   "guardian shutdown",
		})
		cancel()
		if err != nil && status.Code(err) != codes.NotFound {
			errs = append(errs, err)
		}
	}
	for nodeID, conn := range c.nodeConns {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close node %s: %w", nodeID, err))
		}
	}
	c.nodeAddrs = nil
	if c.routerConn != nil {
		if err := c.routerConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (c *GRPCClient) stopHeartbeatLoop() {
	c.stopOnce.Do(func() {
		if c.stopHeartbeat != nil {
			close(c.stopHeartbeat)
		}
		c.heartbeatWG.Wait()
	})
}

func (c *GRPCClient) UpsertPaths(ctx context.Context, token string, writes []guardianapi.PathWrite, mutationCtx guardianapi.MutationContext) (guardianapi.BatchRevision, error) {
	const maxChunkBytes = 8 * 1024 * 1024
	const maxChunkFiles = 64

	return retryRouterCall(c, ctx, func() (guardianapi.BatchRevision, error) {
		callCtx, cancel := c.withTimeout(ctx)
		defer cancel()

		stream, err := c.router.UpsertGuardianPathsStream(callCtx)
		if err != nil {
			return guardianapi.BatchRevision{}, mapRPCError(err)
		}

		var chunk []*pb.GuardianPathWrite
		var chunkBytes int
		firstChunk := true

		for i, write := range writes {
			pw := &pb.GuardianPathWrite{
				LogicalPath:       write.LogicalPath,
				Content:           write.Content,
				ExpectedVersionId: write.ExpectedVersionID,
			}
			chunk = append(chunk, pw)
			chunkBytes += len(write.Content)
			isLast := i == len(writes)-1

			if chunkBytes >= maxChunkBytes || len(chunk) >= maxChunkFiles || isLast {
				msg := &pb.GuardianPathWriteChunk{
					Writes: chunk,
					IsLast: isLast,
				}
				if firstChunk {
					msg.GuardianToken = token
					msg.Context = toProtoMutationContext(mutationCtx)
					firstChunk = false
				}
				if err := stream.Send(msg); err != nil {
					return guardianapi.BatchRevision{}, mapRPCError(err)
				}
				chunk = nil
				chunkBytes = 0
			}
		}

		resp, err := stream.CloseAndRecv()
		if err != nil {
			return guardianapi.BatchRevision{}, mapRPCError(err)
		}
		if resp != nil && !resp.GetSuccess() {
			return guardianapi.BatchRevision{}, fmt.Errorf("monofs upsert failed: %s", resp.GetMessage())
		}
		return guardianapi.BatchRevision{
			BatchRevisionID: resp.GetBatchRevisionId(),
			Files:           convertFileVersions(resp.GetVersions()),
		}, nil
	})
}

func (c *GRPCClient) DeletePaths(ctx context.Context, token string, deletes []guardianapi.PathDelete, mutationCtx guardianapi.MutationContext) (guardianapi.BatchRevision, error) {
	req := &pb.DeleteGuardianPathsRequest{
		GuardianToken: token,
		Context:       toProtoMutationContext(mutationCtx),
		Deletes:       make([]*pb.GuardianPathDelete, 0, len(deletes)),
	}
	for _, del := range deletes {
		req.Deletes = append(req.Deletes, &pb.GuardianPathDelete{
			LogicalPath:       del.LogicalPath,
			ExpectedVersionId: del.ExpectedVersionID,
		})
	}

	return retryRouterCall(c, ctx, func() (guardianapi.BatchRevision, error) {
		callCtx, cancel := c.withTimeout(ctx)
		defer cancel()
		resp, err := c.router.DeleteGuardianPaths(callCtx, req)
		if err != nil {
			return guardianapi.BatchRevision{}, mapRPCError(err)
		}
		if resp != nil && !resp.GetSuccess() {
			return guardianapi.BatchRevision{}, fmt.Errorf("monofs delete failed: %s", resp.GetMessage())
		}
		return guardianapi.BatchRevision{
			BatchRevisionID: resp.GetBatchRevisionId(),
			Files:           convertFileVersions(resp.GetTombstones()),
		}, nil
	})
}

func (c *GRPCClient) ListVersions(ctx context.Context, token, logicalPath string) ([]guardianapi.FileVersion, error) {
	return retryRouterCall(c, ctx, func() ([]guardianapi.FileVersion, error) {
		var (
			pageToken string
			out       []guardianapi.FileVersion
		)
		for {
			callCtx, cancel := c.withTimeout(ctx)
			resp, err := c.router.ListGuardianVersions(callCtx, &pb.ListGuardianVersionsRequest{
				GuardianToken: token,
				LogicalPath:   logicalPath,
				PageSize:      256,
				PageToken:     pageToken,
			})
			cancel()
			if err != nil {
				return nil, mapRPCError(err)
			}
			out = append(out, convertFileVersions(resp.GetVersions())...)
			pageToken = resp.GetNextPageToken()
			if pageToken == "" {
				return out, nil
			}
		}
	})
}

func (c *GRPCClient) GetVersion(ctx context.Context, token, logicalPath, versionID string) (guardianapi.VersionedFile, error) {
	return retryRouterCall(c, ctx, func() (guardianapi.VersionedFile, error) {
		callCtx, cancel := c.withTimeout(ctx)
		defer cancel()
		resp, err := c.router.GetGuardianVersion(callCtx, &pb.GetGuardianVersionRequest{
			GuardianToken: token,
			LogicalPath:   logicalPath,
			VersionId:     versionID,
		})
		if err != nil {
			return guardianapi.VersionedFile{}, mapRPCError(err)
		}
		return guardianapi.VersionedFile{
			Version: convertFileVersion(resp.GetVersion()),
			Content: append([]byte(nil), resp.GetContent()...),
		}, nil
	})
}

func (c *GRPCClient) ReadFile(ctx context.Context, mountPath string) ([]byte, error) {
	nodes, err := c.healthyNodes(ctx)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, node := range nodes {
		callCtx, cancel := c.withTimeout(ctx)
		stream, err := node.client.Read(callCtx, &pb.ReadRequest{
			Path:   mountPath,
			Offset: 0,
			Size:   0,
		})
		if err != nil {
			cancel()
			lastErr = err
			c.invalidateNodeOnTransportError(node.id, err)
			continue
		}
		content, readErr := readAll(stream)
		cancel()
		if readErr == nil {
			return content, nil
		}
		lastErr = readErr
		c.invalidateNodeOnTransportError(node.id, readErr)
	}
	if lastErr == nil || status.Code(lastErr) == codes.NotFound {
		return nil, os.ErrNotExist
	}
	return nil, wrapDNSHint(lastErr)
}

func (c *GRPCClient) ListDir(ctx context.Context, mountPath string) ([]guardianapi.DirEntry, error) {
	nodes, err := c.healthyNodes(ctx)
	if err != nil {
		return nil, err
	}

	entries := map[string]guardianapi.DirEntry{}
	var (
		lastErr error
		success bool
	)
	for _, node := range nodes {
		callCtx, cancel := c.withTimeout(ctx)
		stream, err := node.client.ReadDir(callCtx, &pb.ReadDirRequest{Path: mountPath})
		if err != nil {
			cancel()
			c.invalidateNodeOnTransportError(node.id, err)
			if status.Code(err) != codes.NotFound {
				lastErr = err
			}
			continue
		}
		success = true
		for {
			entry, recvErr := stream.Recv()
			if recvErr == io.EOF {
				break
			}
			if recvErr != nil {
				lastErr = recvErr
				c.invalidateNodeOnTransportError(node.id, recvErr)
				break
			}
			name := entry.GetName()
			if name == "" {
				continue
			}
			current := guardianapi.DirEntry{
				Name:  name,
				IsDir: entry.GetMode()&0o040000 != 0,
			}
			existing, ok := entries[name]
			if ok && existing.IsDir {
				continue
			}
			if current.IsDir && ok {
				current.Size = existing.Size
			}
			entries[name] = current
		}
		cancel()
	}

	if len(entries) == 0 {
		if success {
			return nil, nil
		}
		if lastErr == nil || status.Code(lastErr) == codes.NotFound {
			return nil, nil
		}
		return nil, wrapDNSHint(lastErr)
	}

	out := make([]guardianapi.DirEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *GRPCClient) ListDirPage(ctx context.Context, mountPath string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	entries, err := c.ListDir(ctx, mountPath)
	if err != nil {
		return guardianapi.DirListPage{}, err
	}
	return paginateDirEntries(entries, opts)
}

func paginateDirEntries(entries []guardianapi.DirEntry, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	if opts.Offset < 0 {
		return guardianapi.DirListPage{}, fmt.Errorf("offset must be zero or greater")
	}
	if opts.Limit < 0 {
		return guardianapi.DirListPage{}, fmt.Errorf("limit must be zero or greater")
	}
	if opts.Offset >= len(entries) {
		return guardianapi.DirListPage{Entries: nil, NextOffset: opts.Offset, HasMore: false}, nil
	}
	end := len(entries)
	if opts.Limit > 0 && opts.Offset+opts.Limit < end {
		end = opts.Offset + opts.Limit
	}
	page := guardianapi.DirListPage{
		Entries: append([]guardianapi.DirEntry(nil), entries[opts.Offset:end]...),
	}
	if end < len(entries) {
		page.HasMore = true
		page.NextOffset = end
	} else {
		page.NextOffset = end
	}
	return page, nil
}

func (c *GRPCClient) Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	callCtx, cancel := context.WithCancel(ctx)

	stream, err := retryRouterCall(c, ctx, func() (grpc.ServerStreamingClient[pb.GuardianChangeEvent], error) {
		return c.router.SubscribeGuardianChanges(callCtx, &pb.SubscribeGuardianChangesRequest{
			GuardianToken:        c.token,
			LogicalPrefixes:      prefixes,
			IncludeInlineContent: false,
		})
	})
	if err != nil {
		cancel()
		return nil, mapRPCError(err)
	}

	out := make(chan guardianapi.ChangeEvent, 64)
	go func() {
		defer cancel()
		defer close(out)
		for {
			event, recvErr := stream.Recv()
			if recvErr == io.EOF || errors.Is(recvErr, context.Canceled) {
				return
			}
			if recvErr != nil {
				return
			}
			change := guardianapi.ChangeEvent{
				LogicalPath:     event.GetLogicalPath(),
				Type:            fromProtoChangeType(event.GetType()),
				VersionID:       event.GetVersionId(),
				BatchRevisionID: event.GetBatchRevisionId(),
				CommittedAt:     time.Unix(event.GetCommittedAt(), 0).UTC(),
			}
			select {
			case <-ctx.Done():
				return
			case out <- change:
			}
		}
	}()
	return out, nil
}

func (c *GRPCClient) buildRegisterRequest() *pb.RegisterClientRequest {
	return &pb.RegisterClientRequest{
		ClientId:   c.clientID,
		MountPoint: c.mountPoint,
		Hostname:   c.hostname,
		Writable:   c.writable,
		Version:    c.version,
		GuardianConfig: &pb.GuardianConfig{
			BaseUrl:     c.baseURL,
			AuthToken:   c.token,
			PrincipalId: c.principalID,
			Role:        c.role,
			DisplayName: c.clientID,
		},
	}
}

func registerHeartbeatInterval(resp *pb.RegisterClientResponse) (time.Duration, error) {
	if resp == nil {
		return 0, fmt.Errorf("empty register response")
	}
	if !resp.GetSuccess() {
		message := strings.TrimSpace(resp.GetMessage())
		if message == "" {
			message = "guardian client registration rejected"
		}
		return 0, errors.New(message)
	}

	interval := time.Duration(resp.GetHeartbeatIntervalMs()) * time.Millisecond
	if interval <= 0 {
		return defaultClientHeartbeatInterval, nil
	}
	return interval, nil
}

func (c *GRPCClient) register(ctx context.Context) (time.Duration, error) {
	callCtx, cancel := c.withTimeout(ctx)
	defer cancel()

	resp, err := c.router.RegisterClient(callCtx, c.buildRegisterRequest())
	if err != nil {
		return 0, fmt.Errorf("register guardian client with monofs: %w", mapRPCError(err))
	}
	heartbeatInterval, err := registerHeartbeatInterval(resp)
	if err != nil {
		return 0, fmt.Errorf("register guardian client with monofs: %w", err)
	}
	return heartbeatInterval, nil
}

func (c *GRPCClient) startHeartbeatLoop(interval time.Duration) {
	if interval <= 0 {
		interval = defaultClientHeartbeatInterval
	}

	c.heartbeatWG.Add(1)
	go func() {
		defer c.heartbeatWG.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.stopHeartbeat:
				return
			case <-ticker.C:
				c.sendHeartbeat()
			}
		}
	}()
}

func (c *GRPCClient) sendHeartbeat() {
	callCtx, cancel := c.withTimeout(context.Background())
	defer cancel()

	resp, err := c.router.ClientHeartbeat(callCtx, &pb.ClientHeartbeatRequest{
		ClientId: c.clientID,
	})
	if err != nil {
		return
	}
	if resp.GetShouldRegister() {
		_, _ = c.register(context.Background())
	}
}

func (c *GRPCClient) healthyNodes(ctx context.Context) ([]nodeTarget, error) {
	if nodes, ok := c.cachedHealthyNodes(); ok {
		return nodes, nil
	}
	const maxRetries = 4
	const baseBackoff = 500 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			if isTransportUnavailable(lastErr) {
				_ = c.reconnectRouter(ctx)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(baseBackoff * time.Duration(attempt)):
			}
		}
		if err := c.refreshNodes(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			nodes := c.snapshotNodeTargets()
			if len(nodes) > 0 {
				return nodes, nil
			}
			continue
		}
		nodes := c.snapshotNodeTargets()
		if len(nodes) == 0 {
			lastErr = fmt.Errorf("no healthy monofs nodes available")
			continue
		}
		return nodes, nil
	}
	return nil, lastErr
}

func (c *GRPCClient) cachedHealthyNodes() ([]nodeTarget, bool) {
	ttl := c.refreshTTL
	if ttl <= 0 {
		ttl = defaultTopologyRefreshInterval
	}
	c.mu.Lock()
	fresh := !c.lastRefresh.IsZero() && time.Since(c.lastRefresh) <= ttl && len(c.nodeClients) > 0
	if !fresh {
		c.mu.Unlock()
		return nil, false
	}
	nodes := c.snapshotNodeTargetsLocked()
	c.mu.Unlock()
	if len(nodes) == 0 {
		return nil, false
	}
	return nodes, true
}

func (c *GRPCClient) snapshotNodeTargets() []nodeTarget {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotNodeTargetsLocked()
}

func (c *GRPCClient) snapshotNodeTargetsLocked() []nodeTarget {
	ids := make([]string, 0, len(c.nodeClients))
	for id := range c.nodeClients {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]nodeTarget, 0, len(ids))
	for _, id := range ids {
		out = append(out, nodeTarget{id: id, client: c.nodeClients[id]})
	}
	return out
}

func (c *GRPCClient) rewriteNodeAddr(addr string) string {
	if !c.useExternalAddresses {
		return addr
	}
	ip := net.ParseIP(c.routerHost)
	if ip == nil || !ip.IsLoopback() {
		return addr
	}
	nodeHost, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if nodeHost == c.routerHost {
		return addr
	}
	return net.JoinHostPort(c.routerHost, port)
}

func (c *GRPCClient) refreshNodes(ctx context.Context) error {
	callCtx, cancel := c.withTimeout(ctx)
	defer cancel()

	resp, err := c.router.GetClusterInfo(callCtx, &pb.ClusterInfoRequest{
		ClientId:             c.clientID,
		UseExternalAddresses: c.useExternalAddresses,
	})
	if err != nil {
		return fmt.Errorf("fetch monofs cluster info: %w", wrapDNSHint(err))
	}

	healthy := make([]*pb.NodeInfo, 0, len(resp.GetNodes()))
	for _, node := range resp.GetNodes() {
		if node == nil || !node.GetHealthy() {
			continue
		}
		if node.GetNodeId() == "" || node.GetAddress() == "" {
			continue
		}
		healthy = append(healthy, node)
	}
	if len(healthy) == 0 {
		return fmt.Errorf("no healthy monofs nodes available")
	}

	c.mu.Lock()
	if c.nodeConns == nil {
		c.nodeConns = map[string]*grpc.ClientConn{}
	}
	if c.nodeClients == nil {
		c.nodeClients = map[string]pb.MonoFSClient{}
	}
	if c.nodeAddrs == nil {
		c.nodeAddrs = map[string]string{}
	}
	c.mu.Unlock()

	seen := map[string]struct{}{}
	connected := 0
	type staleConn struct {
		nodeID string
		conn   *grpc.ClientConn
	}
	var staleConns []staleConn
	var connectErr error

	for _, node := range healthy {
		nodeID := node.GetNodeId()
		nodeAddr := c.rewriteNodeAddr(node.GetAddress())
		seen[nodeID] = struct{}{}

		c.mu.Lock()
		conn := c.nodeConns[nodeID]
		sameAddr := conn != nil && c.nodeAddrs[nodeID] == nodeAddr
		c.mu.Unlock()

		if sameAddr {
			connected++
			continue
		}
		if conn != nil {
			staleConns = append(staleConns, staleConn{nodeID: nodeID, conn: conn})
			c.mu.Lock()
			delete(c.nodeConns, nodeID)
			delete(c.nodeClients, nodeID)
			delete(c.nodeAddrs, nodeID)
			c.mu.Unlock()
		}
		nodeConn, dialErr := grpc.NewClient(
			nodeAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             10 * time.Second,
				PermitWithoutStream: true,
			}),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(1024*1024*1024),
				grpc.MaxCallSendMsgSize(1024*1024*1024),
			),
		)
		if dialErr != nil {
			connectErr = fmt.Errorf("connect to monofs node %s: %w", nodeID, dialErr)
			continue
		}
		c.mu.Lock()
		c.nodeConns[nodeID] = nodeConn
		c.nodeClients[nodeID] = pb.NewMonoFSClient(nodeConn)
		c.nodeAddrs[nodeID] = nodeAddr
		connected++
		c.mu.Unlock()
	}

	c.mu.Lock()
	for nodeID, conn := range c.nodeConns {
		if _, ok := seen[nodeID]; ok {
			continue
		}
		staleConns = append(staleConns, staleConn{nodeID: nodeID, conn: conn})
		delete(c.nodeConns, nodeID)
		delete(c.nodeClients, nodeID)
		delete(c.nodeAddrs, nodeID)
	}
	c.lastRefresh = time.Now()
	c.mu.Unlock()

	go func() {
		for _, sc := range staleConns {
			_ = sc.conn.Close()
		}
	}()

	if connected == 0 {
		if connectErr != nil {
			return connectErr
		}
		return fmt.Errorf("no healthy monofs nodes available")
	}
	return nil
}

func (c *GRPCClient) invalidateNodeOnTransportError(nodeID string, err error) {
	if nodeID == "" || !isTransportUnavailable(err) {
		return
	}
	c.mu.Lock()
	if conn := c.nodeConns[nodeID]; conn != nil {
		_ = conn.Close()
	}
	delete(c.nodeConns, nodeID)
	delete(c.nodeClients, nodeID)
	delete(c.nodeAddrs, nodeID)
	c.lastRefresh = time.Time{}
	c.mu.Unlock()
	go c.tryBackgroundRefresh()
}

func (c *GRPCClient) tryBackgroundRefresh() {
	if c.router == nil {
		return
	}
	c.refreshMu.Lock()
	if c.refreshing {
		c.refreshMu.Unlock()
		return
	}
	c.refreshing = true
	c.refreshMu.Unlock()

	defer func() {
		c.refreshMu.Lock()
		c.refreshing = false
		c.refreshMu.Unlock()
	}()

	const maxAttempts = 4
	const backoff = 1 * time.Second
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff * time.Duration(attempt))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := c.refreshNodes(ctx)
		cancel()
		if err == nil {
			return
		}
	}
}

func (c *GRPCClient) reconnectRouter(ctx context.Context) error {
	c.routerMu.Lock()
	defer c.routerMu.Unlock()

	oldConn := c.routerConn

	newConn, err := dialRouter(c.routerAddr)
	if err != nil {
		return fmt.Errorf("reconnect to monofs router: %w", err)
	}

	c.routerConn = newConn
	c.router = pb.NewMonoFSRouterClient(newConn)

	if oldConn != nil {
		_ = oldConn.Close()
	}

	callCtx, cancel := c.withTimeout(ctx)
	defer cancel()
	resp, regErr := c.router.RegisterClient(callCtx, c.buildRegisterRequest())
	if regErr != nil {
		return nil
	}
	hbInterval, hbErr := registerHeartbeatInterval(resp)
	if hbErr != nil {
		return nil
	}
	if hbInterval <= 0 {
		hbInterval = defaultClientHeartbeatInterval
	}
	return nil
}

const retryMaxAttempts = 3
const retryBackoff = 500 * time.Millisecond

func retryRouterCall[T any](c *GRPCClient, ctx context.Context, fn func() (T, error)) (T, error) {
	var lastErr error
	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		if attempt > 0 {
			if ctx.Err() != nil {
				var zero T
				return zero, ctx.Err()
			}
			select {
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			case <-time.After(retryBackoff * time.Duration(attempt)):
			}
			if reconnectErr := c.reconnectRouter(ctx); reconnectErr != nil {
				lastErr = reconnectErr
				continue
			}
		}
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isTransportUnavailable(err) {
			return result, err
		}
	}
	var zero T
	return zero, lastErr
}

func isTransportUnavailable(err error) bool {
	if err == nil {
		return false
	}
	code := status.Code(err)
	if code == codes.Unavailable {
		return true
	}
	message := strings.ToLower(err.Error())
	if code == codes.Canceled && strings.Contains(message, "client connection is closing") {
		return true
	}
	return strings.Contains(message, "error reading server preface") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "transport is closing") ||
		strings.Contains(message, "unexpected eof")
}

func (c *GRPCClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.rpcTimeout)
}

func readAll(stream grpc.ServerStreamingClient[pb.DataChunk]) ([]byte, error) {
	var content []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return content, nil
		}
		if err != nil {
			return nil, err
		}
		content = append(content, chunk.GetData()...)
	}
}

func toProtoMutationContext(ctx guardianapi.MutationContext) *pb.GuardianMutationContext {
	return &pb.GuardianMutationContext{
		PrincipalId:   ctx.PrincipalID,
		Reason:        ctx.Reason,
		CorrelationId: ctx.CorrelationID,
	}
}

func convertFileVersions(in []*pb.GuardianFileVersion) []guardianapi.FileVersion {
	out := make([]guardianapi.FileVersion, 0, len(in))
	for _, version := range in {
		out = append(out, convertFileVersion(version))
	}
	return out
}

func convertFileVersion(in *pb.GuardianFileVersion) guardianapi.FileVersion {
	if in == nil {
		return guardianapi.FileVersion{}
	}
	return guardianapi.FileVersion{
		LogicalPath:     in.GetLogicalPath(),
		VersionID:       in.GetVersionId(),
		BatchRevisionID: in.GetBatchRevisionId(),
		ContentSHA256:   in.GetContentSha256(),
		CommittedAt:     time.Unix(in.GetCommittedAt(), 0).UTC(),
		Tombstone:       in.GetTombstone(),
		PrincipalID:     in.GetPrincipalId(),
		Reason:          in.GetReason(),
	}
}

func fromProtoChangeType(in pb.ChangeType) guardianapi.ChangeType {
	switch in {
	case pb.ChangeType_ADDED:
		return guardianapi.ChangeAdded
	case pb.ChangeType_MODIFIED:
		return guardianapi.ChangeModified
	case pb.ChangeType_DELETED:
		return guardianapi.ChangeDeleted
	default:
		return guardianapi.ChangeModified
	}
}

func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	code := status.Code(err)
	switch code {
	case codes.AlreadyExists, codes.FailedPrecondition:
		return guardianapi.ErrConflict
	case codes.NotFound:
		return os.ErrNotExist
	case codes.Unavailable:
		return wrapDNSHint(err)
	default:
		return err
	}
}

func wrapDNSHint(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "name resolver") || strings.Contains(msg, "produced zero addresses") || strings.Contains(msg, "DNS resolution") {
		return fmt.Errorf("%w\n\n  DNS resolution failed for a MonoFS node.\n"+
			"  The node addresses returned by the router are K8s-internal names\n"+
			"  (e.g. node-a-external.monofs.svc.cluster.local) that cannot be resolved\n"+
			"  from outside the cluster.\n\n"+
			"  Fixes:\n"+
			"    - Pass an IP address as --monofs-router (node ports will use the same host)\n"+
			"    - Add entries to /etc/hosts for the K8s service names\n"+
			"    - Run guardianctl from inside the cluster", err)
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "\"TRANSIENT_FAILURE\"") {
		return fmt.Errorf("%w\n\n  Connection to a MonoFS node was refused.\n"+
			"  Verify the node's gRPC port is reachable from this host.", err)
	}
	return err
}
