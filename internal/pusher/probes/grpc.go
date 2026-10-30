package probes

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type grpcDescriptorCache struct {
	services map[string]*protoregistry.Files
}

func newGRPCCache() *grpcDescriptorCache {
	return &grpcDescriptorCache{services: map[string]*protoregistry.Files{}}
}

func (c *grpcDescriptorCache) get(key string) *protoregistry.Files {
	if c == nil {
		return nil
	}
	return c.services[key]
}

func (c *grpcDescriptorCache) set(key string, files *protoregistry.Files) {
	if c == nil || files == nil {
		return
	}
	c.services[key] = files
}

type grpcStepResult struct {
	Code        codes.Code
	Body        []byte
	Target      string
	headerValue func(string) (string, bool)
}

type grpcTarget struct {
	address    string
	tls        bool
	serverName string
}

// probeGRPCHealth is the single-request gRPC probe path. It performs a
// standard grpc.health.v1.Health/Check instead of only opening a TCP socket.
func (r *Runner) probeGRPCHealth(ctx context.Context, target Target) (int, string, error) {
	step := Step{
		Name:     target.Name,
		Type:     "grpc",
		URL:      target.URL,
		Address:  target.Address,
		Port:     target.Port,
		Insecure: target.Insecure,
	}
	resolved, err := resolveGRPCTarget(target, step, newJourneyVars(target))
	if err != nil {
		return 0, "", err
	}
	conn, err := dialGRPC(resolved, target.Insecure)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	out, err := runGRPCHealth(ctx, conn, resolved, step, "")
	return int(out.Code), "", err
}

func (r *Runner) runGRPCStep(ctx context.Context, base Target, step Step, state *journeyState) (grpcStepResult, error) {
	target, err := resolveGRPCTarget(base, step, state.vars)
	if err != nil {
		return grpcStepResult{}, err
	}
	conn, err := dialGRPC(target, step.Insecure)
	if err != nil {
		return grpcStepResult{}, err
	}
	defer conn.Close()

	service, err := state.vars.expandString(strings.TrimSpace(step.Service))
	if err != nil {
		return grpcStepResult{}, err
	}
	service = strings.TrimSpace(service)
	method, err := state.vars.expandString(strings.TrimSpace(step.GRPCMethod))
	if err != nil {
		return grpcStepResult{}, err
	}
	method = strings.TrimSpace(method)
	if service == "" || method == "" {
		// No method means a standard gRPC health check. The service field, when
		// set, selects the health service name to check.
		return runGRPCHealth(ctx, conn, target, step, service)
	}
	return runGRPCDynamic(ctx, conn, target, step, state, service, method)
}

func resolveGRPCTarget(base Target, step Step, vars *journeyVars) (grpcTarget, error) {
	rawURL := strings.TrimSpace(step.URL)
	if rawURL == "" {
		rawURL = strings.TrimSpace(base.URL)
	}
	address := ""
	scheme := ""
	if rawURL != "" {
		expanded, err := vars.expandString(rawURL)
		if err != nil {
			return grpcTarget{}, err
		}
		if strings.Contains(expanded, "://") {
			parsed, err := url.Parse(expanded)
			if err != nil {
				return grpcTarget{}, fmt.Errorf("parse grpc url: %w", err)
			}
			scheme = strings.ToLower(parsed.Scheme)
			address = parsed.Host
		} else {
			address = expanded
		}
	}
	if address == "" {
		address = firstNonEmpty(step.Address, base.Address)
		if address != "" {
			expanded, err := vars.expandString(address)
			if err != nil {
				return grpcTarget{}, err
			}
			address = expanded
		}
	}
	if strings.TrimSpace(address) == "" {
		return grpcTarget{}, fmt.Errorf("grpc step requires url or address")
	}
	port := step.Port
	if port == 0 {
		port = base.Port
	}
	host := address
	existingPort := 0
	if parsedHost, parsedPort, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
		if value, err := strconv.Atoi(parsedPort); err == nil {
			existingPort = value
		}
	} else if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		host = strings.Trim(address, "[]")
	}
	if existingPort == 0 {
		existingPort = port
	}
	tlsEnabled := scheme == "https" || scheme == "grpcs" || existingPort == 443
	if scheme == "http" || scheme == "grpc" {
		tlsEnabled = false
	}
	if existingPort == 0 {
		if tlsEnabled {
			existingPort = 443
		} else {
			existingPort = 80
		}
	}
	address = net.JoinHostPort(host, strconv.Itoa(existingPort))
	return grpcTarget{address: address, tls: tlsEnabled, serverName: host}, nil
}

func dialGRPC(target grpcTarget, skipVerify bool) (*grpc.ClientConn, error) {
	var creds credentials.TransportCredentials
	if target.tls {
		creds = credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: skipVerify, //nolint:gosec
			ServerName:         target.serverName,
		})
	} else {
		creds = insecure.NewCredentials()
	}
	return grpc.NewClient(target.address, grpc.WithTransportCredentials(creds))
}

func runGRPCHealth(ctx context.Context, conn *grpc.ClientConn, target grpcTarget, step Step, service string) (grpcStepResult, error) {
	client := healthpb.NewHealthClient(conn)
	var header, trailer metadata.MD
	resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{Service: service}, grpc.Header(&header), grpc.Trailer(&trailer))
	code := status.Code(err)
	body, _ := protojson.Marshal(resp)
	out := grpcStepResult{
		Code:        code,
		Body:        body,
		Target:      target.address,
		headerValue: metadataGetter(header, trailer),
	}
	if err != nil {
		if !expectedGRPCCode(code, step.ExpectStatus) {
			return out, err
		}
		return out, nil
	}
	if !expectedGRPCCode(code, step.ExpectStatus) {
		return out, fmt.Errorf("unexpected gRPC status %d (%s)", int(code), code)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return out, fmt.Errorf("gRPC health status %s", resp.GetStatus())
	}
	return out, nil
}

func runGRPCDynamic(ctx context.Context, conn *grpc.ClientConn, target grpcTarget, step Step, state *journeyState, service, method string) (grpcStepResult, error) {
	methodDescriptor, err := resolveGRPCMethod(ctx, conn, target.address, service, method, state.grpc)
	if err != nil {
		return grpcStepResult{Target: target.address}, err
	}
	requestJSON, err := encodeGRPCRequest(step.Request, step.Patch, state.vars)
	if err != nil {
		return grpcStepResult{Target: target.address}, err
	}
	request := dynamicpb.NewMessage(methodDescriptor.Input())
	if len(requestJSON) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(requestJSON, request); err != nil {
			return grpcStepResult{Target: target.address}, fmt.Errorf("decode grpc request: %w", err)
		}
	}
	response := dynamicpb.NewMessage(methodDescriptor.Output())
	callCtx, err := metadataContext(ctx, step.Metadata, state.vars)
	if err != nil {
		return grpcStepResult{Target: target.address}, err
	}
	var header, trailer metadata.MD
	callErr := conn.Invoke(callCtx, "/"+service+"/"+string(methodDescriptor.Name()), request, response,
		grpc.Header(&header), grpc.Trailer(&trailer))
	code := status.Code(callErr)
	body, marshalErr := protojson.Marshal(response)
	out := grpcStepResult{
		Code:        code,
		Body:        body,
		Target:      target.address,
		headerValue: metadataGetter(header, trailer),
	}
	if marshalErr != nil && callErr == nil {
		return out, marshalErr
	}
	if !expectedGRPCCode(code, step.ExpectStatus) {
		if callErr != nil {
			return out, callErr
		}
		return out, fmt.Errorf("unexpected gRPC status %d (%s)", int(code), code)
	}
	return out, nil
}

func metadataContext(ctx context.Context, values map[string]string, vars *journeyVars) (context.Context, error) {
	expanded, err := expandStringMap(values, vars)
	if err != nil {
		return ctx, err
	}
	if len(expanded) == 0 {
		return ctx, nil
	}
	pairs := make(map[string]string, len(expanded))
	for key, value := range expanded {
		pairs[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return metadata.NewOutgoingContext(ctx, metadata.New(pairs)), nil
}

func metadataGetter(header, trailer metadata.MD) func(string) (string, bool) {
	return func(name string) (string, bool) {
		name = strings.ToLower(strings.TrimSpace(name))
		if values := header.Get(name); len(values) > 0 {
			return values[0], true
		}
		if values := trailer.Get(name); len(values) > 0 {
			return values[0], true
		}
		return "", false
	}
}

func expectedGRPCCode(code codes.Code, expected []int) bool {
	if len(expected) == 0 {
		return code == codes.OK
	}
	for _, value := range expected {
		if int(code) == value {
			return true
		}
	}
	return false
}

func resolveGRPCMethod(ctx context.Context, conn *grpc.ClientConn, address, service, method string, cache *grpcDescriptorCache) (protoreflect.MethodDescriptor, error) {
	key := address + "|" + service
	files := cache.get(key)
	if files == nil {
		descriptors, err := fetchServiceDescriptors(ctx, conn, service)
		if err != nil {
			return nil, err
		}
		files, err = protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: descriptors})
		if err != nil {
			return nil, fmt.Errorf("build gRPC descriptors for %s: %w", service, err)
		}
		cache.set(key, files)
	}
	descriptor, err := files.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("gRPC service %q not found: %w", service, err)
	}
	serviceDescriptor, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q is not a gRPC service", service)
	}
	methodName := strings.TrimSpace(method)
	if idx := strings.LastIndex(methodName, "/"); idx >= 0 {
		methodName = methodName[idx+1:]
	}
	if strings.HasPrefix(methodName, service+".") {
		methodName = strings.TrimPrefix(methodName, service+".")
	}
	methodDescriptor := serviceDescriptor.Methods().ByName(protoreflect.Name(methodName))
	if methodDescriptor == nil {
		return nil, fmt.Errorf("gRPC method %q not found on %s (server reflection must be enabled)", method, service)
	}
	return methodDescriptor, nil
}

func fetchServiceDescriptors(ctx context.Context, conn *grpc.ClientConn, service string) ([]*descriptorpb.FileDescriptorProto, error) {
	descriptors, err := fetchServiceDescriptorsV1(ctx, conn, service)
	if err == nil {
		return descriptors, nil
	}
	if status.Code(err) != codes.Unimplemented {
		return nil, err
	}
	return fetchServiceDescriptorsV1Alpha(ctx, conn, service)
}

func fetchServiceDescriptorsV1(ctx context.Context, conn *grpc.ClientConn, service string) ([]*descriptorpb.FileDescriptorProto, error) {
	client := reflectionv1.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.CloseSend()
	if err := stream.Send(&reflectionv1.ServerReflectionRequest{
		MessageRequest: &reflectionv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: service},
	}); err != nil {
		return nil, err
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if reflectionErr := response.GetErrorResponse(); reflectionErr != nil {
		return nil, status.Error(codes.Code(reflectionErr.GetErrorCode()), fmt.Sprintf("gRPC reflection: %s", reflectionErr.GetErrorMessage()))
	}
	fileResponse := response.GetFileDescriptorResponse()
	if fileResponse == nil {
		return nil, fmt.Errorf("gRPC reflection returned no file descriptors for %s", service)
	}
	return decodeFileDescriptors(fileResponse.GetFileDescriptorProto())
}

func fetchServiceDescriptorsV1Alpha(ctx context.Context, conn *grpc.ClientConn, service string) ([]*descriptorpb.FileDescriptorProto, error) {
	client := reflectionv1alpha.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.CloseSend()
	if err := stream.Send(&reflectionv1alpha.ServerReflectionRequest{
		MessageRequest: &reflectionv1alpha.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: service},
	}); err != nil {
		return nil, err
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	if reflectionErr := response.GetErrorResponse(); reflectionErr != nil {
		return nil, status.Error(codes.Code(reflectionErr.GetErrorCode()), fmt.Sprintf("gRPC reflection: %s", reflectionErr.GetErrorMessage()))
	}
	fileResponse := response.GetFileDescriptorResponse()
	if fileResponse == nil {
		return nil, fmt.Errorf("gRPC reflection returned no file descriptors for %s", service)
	}
	return decodeFileDescriptors(fileResponse.GetFileDescriptorProto())
}

func decodeFileDescriptors(raw [][]byte) ([]*descriptorpb.FileDescriptorProto, error) {
	descriptors := make([]*descriptorpb.FileDescriptorProto, 0, len(raw))
	for idx, payload := range raw {
		var descriptor descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(payload, &descriptor); err != nil {
			return nil, fmt.Errorf("decode reflection file descriptor %d: %w", idx, err)
		}
		descriptors = append(descriptors, &descriptor)
	}
	if len(descriptors) == 0 {
		return nil, fmt.Errorf("gRPC reflection returned an empty descriptor set")
	}
	return descriptors, nil
}
