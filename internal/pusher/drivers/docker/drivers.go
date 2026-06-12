package dockerdriver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	orchestratorcommon "github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

type baseDriver struct {
	backend           BackendAPI
	resolver          secrets.Resolver
	defaultExtraHosts map[string]string
}

type VolumeDriver struct{ baseDriver }
type ConfigDriver struct{ baseDriver }
type NetworkDriver struct{ baseDriver }
type ComputeDriver struct{ baseDriver }
type TraefikRouteDriver struct{ baseDriver }
type LoadBalancerDriver struct{ baseDriver }
type ObjectStoreDriver struct{ baseDriver }
type SQLDatabaseDriver struct {
	baseDriver
	typeName string
}
type ObservabilityDriver struct{ baseDriver }

type Option func(*baseDriver)

func WithDefaultExtraHosts(hosts map[string]string) Option {
	copied := cloneStringMap(hosts)
	return func(d *baseDriver) {
		d.defaultExtraHosts = cloneStringMap(copied)
	}
}

func Register(reg *registry.Registry, backend BackendAPI, resolver secrets.Resolver, options ...Option) {
	if reg == nil {
		return
	}
	if backend == nil {
		backend = NewBackend()
	}
	if resolver == nil {
		resolver = secrets.NoopResolver{}
	}
	base := baseDriver{backend: backend, resolver: resolver}
	for _, option := range options {
		if option != nil {
			option(&base)
		}
	}
	reg.Register(&VolumeDriver{base})
	reg.Register(&ConfigDriver{base})
	reg.Register(&NetworkDriver{base})
	reg.Register(&ComputeDriver{base})
	reg.Register(&ImageBuildDriver{baseDriver: base, backend: NewImageBuildBackend(), defaultRegistry: strings.TrimSpace(os.Getenv("GUARDIAN_IMAGE_BUILD_REGISTRY"))})
	reg.Register(&TraefikRouteDriver{base})
	reg.Register(&LoadBalancerDriver{base})
	reg.Register(&ObjectStoreDriver{base})
	reg.Register(&SQLDatabaseDriver{baseDriver: base, typeName: "Database"})
	reg.Register(&SQLDatabaseDriver{baseDriver: base, typeName: "SQLDatabase"})
	reg.Register(&ObservabilityDriver{base})
}

func (d *VolumeDriver) Type() string                         { return "Volume" }
func (d *ConfigDriver) Type() string                         { return "Config" }
func (d *NetworkDriver) Type() string                        { return "Network" }
func (d *ComputeDriver) Type() string                        { return "Compute" }
func (d *TraefikRouteDriver) Type() string                   { return "TraefikRoute" }
func (d *LoadBalancerDriver) Type() string                   { return "LoadBalancer" }
func (d *ObjectStoreDriver) Type() string                    { return "ObjectStore" }
func (d *SQLDatabaseDriver) Type() string                    { return d.typeName }
func (d *ObservabilityDriver) Type() string                  { return "Observability" }
func (d *VolumeDriver) Validate(map[string]any) error        { return nil }
func (d *ConfigDriver) Validate(map[string]any) error        { return nil }
func (d *NetworkDriver) Validate(map[string]any) error       { return nil }
func (d *ComputeDriver) Validate(map[string]any) error       { return nil }
func (d *TraefikRouteDriver) Validate(map[string]any) error  { return nil }
func (d *LoadBalancerDriver) Validate(map[string]any) error  { return nil }
func (d *ObjectStoreDriver) Validate(map[string]any) error   { return nil }
func (d *SQLDatabaseDriver) Validate(map[string]any) error   { return nil }
func (d *ObservabilityDriver) Validate(map[string]any) error { return nil }

func (d *VolumeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *VolumeDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	volume, ok, err := d.backend.GetVolume(volumeName(in, in.Asset.Name))
	if err != nil {
		return nil, nil, err
	}
	readiness := readyObservation("docker volume is ready")
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "docker volume is missing"}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: fmt.Sprintf("docker volume %s is available", volume.Name)}, readiness, nil
}

func (d *VolumeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadVolumePayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	name := volumeName(in, in.Asset.Name)
	current, ok, err := d.backend.GetVolume(name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	size := firstNonEmpty(payload.Size, spec.Size)
	accessMode := firstNonEmpty(payload.AccessMode, spec.AccessMode)
	ephemeral := driverutil.BoolValue(spec.Ephemeral)
	if payload.Ephemeral != nil {
		ephemeral = *payload.Ephemeral
	}
	if !ok || current.Size != size || current.AccessMode != accessMode || current.Ephemeral != ephemeral {
		return changedDrift(in.Asset.Name, "docker volume differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "docker volume is in sync"), nil
}

func (d *VolumeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeVolume(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadVolumePayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	name := volumeName(in, in.Asset.Name)
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	ephemeral := driverutil.BoolValue(spec.Ephemeral)
	if payload.Ephemeral != nil {
		ephemeral = *payload.Ephemeral
	}
	if err := d.backend.UpsertVolume(Volume{
		Name:       name,
		Hash:       hash,
		Labels:     driverutil.Labels("docker", in, hash),
		Size:       firstNonEmpty(payload.Size, spec.Size),
		AccessMode: firstNonEmpty(payload.AccessMode, spec.AccessMode),
		Ephemeral:  ephemeral,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{"name": name}}, nil
}

func (d *VolumeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteVolume(volumeName(in, in.Asset.Name))
}

func (d *ConfigDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *ConfigDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	config, ok, err := d.backend.GetConfig(configName(in, in.Asset.Name))
	if err != nil {
		return nil, nil, err
	}
	readiness := readyObservation("docker config is ready")
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "docker config is missing"}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: fmt.Sprintf("docker config %s is available", config.Name)}, readiness, nil
}

func (d *ConfigDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadConfigPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	name := configName(in, in.Asset.Name)
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	files := driverutil.ConfigFiles(spec)
	if len(payload.Files) > 0 {
		files = cloneStringMap(payload.Files)
	}
	current, ok, err := d.backend.GetConfig(name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || current.Hash != hash || !equalStringMaps(current.Files, files) {
		return changedDrift(in.Asset.Name, "docker config differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "docker config is in sync"), nil
}

func (d *ConfigDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeConfig(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadConfigPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	name := configName(in, in.Asset.Name)
	files := driverutil.ConfigFiles(spec)
	if len(payload.Files) > 0 {
		files = cloneStringMap(payload.Files)
	}
	if err := d.backend.UpsertConfig(Config{
		Name:   name,
		Hash:   hash,
		Labels: driverutil.Labels("docker", in, hash),
		Files:  files,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{"name": name}}, nil
}

func (d *ConfigDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteConfig(configName(in, in.Asset.Name))
}

func (d *NetworkDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *NetworkDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	network, ok, err := d.backend.GetNetwork(explicitNetworkName(in, in.Asset.Name))
	if err != nil {
		return nil, nil, err
	}
	readiness := readyObservation("docker network is ready")
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "docker network is missing"}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: fmt.Sprintf("docker network %s is available", network.Name)}, readiness, nil
}

func (d *NetworkDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	name := explicitNetworkName(in, in.Asset.Name)
	current, ok, err := d.backend.GetNetwork(name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		return changedDrift(in.Asset.Name, "docker network not found"), nil
	}
	desired := Network{Driver: spec.Driver, Internal: spec.Internal}
	if drifted, reason := StructuralNetworkDrift(desired, current); drifted {
		return changedDrift(in.Asset.Name, "docker network differs: "+reason), nil
	}
	return inSyncDrift(in.Asset.Name, "docker network is in sync"), nil
}

func (d *NetworkDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeNetwork(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	name := explicitNetworkName(in, in.Asset.Name)
	hash := driverutil.AssetHash(in)
	driver := spec.Driver
	if driver == "" {
		driver = "bridge"
	}
	if err := d.backend.EnsureNetwork(Network{
		Name:     name,
		Hash:     hash,
		Labels:   driverutil.Labels("docker", in, hash),
		Driver:   driver,
		Internal: spec.Internal,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{"name": name}}, nil
}

func (d *NetworkDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteNetwork(explicitNetworkName(in, in.Asset.Name))
}

func (d *ComputeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return err
	}
	for _, mount := range spec.VolumeMounts {
		if _, _, err := d.backend.GetVolume(volumeName(in, mount.Volume)); err != nil {
			return err
		}
	}
	for _, mount := range spec.ConfigMounts {
		if _, _, err := d.backend.GetConfig(configName(in, mount.Config)); err != nil {
			return err
		}
	}
	return nil
}

func (d *ComputeDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readiness := readinessFromError(d.Check(ctx, in), "docker compute dependencies resolved")
	spec, err := decodeCompute(in)
	if err != nil {
		return nil, nil, err
	}
	containers, err := d.backend.ListContainersByAsset(in.PartitionName, in.IntentName, in.Asset.Name)
	if err != nil {
		return nil, nil, err
	}
	desiredNames := computeContainerNames(in, spec)
	if len(containers) != len(desiredNames) {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: "docker compute replica set is incomplete"}, readiness, nil
	}
	for _, name := range desiredNames {
		container, ok, err := d.backend.GetContainer(name)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("docker compute container %s is missing", name)}, readiness, nil
		}
		if !container.Running {
			status := taskdomain.HealthUnhealthy
			summary := fmt.Sprintf("docker compute container %s is not running", name)
			if len(desiredNames) > 1 {
				status = taskdomain.HealthDegraded
				summary = fmt.Sprintf("docker compute replica %s is not running", name)
			}
			return &taskdomain.HealthObservation{Status: status, Summary: summary}, readiness, nil
		}
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "docker compute replica set is running"}, readiness, nil
}

func (d *ComputeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	hash := hashWithPayload(computeHash(in, spec), payload)
	network := networkName(in)
	if _, ok, err := d.backend.GetNetwork(network); err != nil {
		return taskdomain.DriftReport{}, err
	} else if !ok {
		return changedDrift(in.Asset.Name, "docker network differs"), nil
	}
	desiredNames := computeContainerNames(in, spec)
	current, err := d.backend.ListContainersByAsset(in.PartitionName, in.IntentName, in.Asset.Name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if len(current) != len(desiredNames) {
		return changedDrift(in.Asset.Name, "docker compute replica set differs"), nil
	}
	// Resolve env here for structural comparison (same as Apply does before UpsertContainer).
	env, err := driverutil.ResolveEnv(ctx, d.resolver, spec.Env)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	for idx, name := range desiredNames {
		actual, ok, err := d.backend.GetContainer(name)
		if err != nil {
			return taskdomain.DriftReport{}, err
		}
		if !ok {
			return changedDrift(in.Asset.Name, "docker compute container not found"), nil
		}
		if !actual.Running {
			return changedDrift(in.Asset.Name, "docker compute container not running"), nil
		}
		// Build the desired Container struct the same way Apply does, then
		// compare fields structurally. This catches image changes, env drift,
		// port changes, mount changes, capability changes etc. that a pure
		// guardian.hash label comparison would miss when containers are
		// recreated or modified outside Guardian.
		desired, err := buildComputeContainer(in, spec, name, idx, hash, network, env, payload)
		if err != nil {
			return taskdomain.DriftReport{}, err
		}
		d.applyContainerDefaults(&desired)
		if drifted, reason := StructuralContainerDrift(desired, actual); drifted {
			return changedDrift(in.Asset.Name, "docker compute container differs: "+reason), nil
		}
	}
	return inSyncDrift(in.Asset.Name, "docker compute is in sync"), nil
}

func (d *ComputeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	network := networkName(in)
	if err := d.backend.EnsureNetwork(Network{Name: network, Labels: map[string]string{"guardian.cluster": in.Target.Cluster}}); err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(computeHash(in, spec), payload)
	env, err := driverutil.ResolveEnv(ctx, d.resolver, spec.Env)
	if err != nil {
		return registry.AssetResult{}, err
	}
	desired := computeContainerNames(in, spec)
	var primary Container
	for idx, name := range desired {
		container, err := buildComputeContainer(in, spec, name, idx, hash, network, env, payload)
		if err != nil {
			return registry.AssetResult{}, err
		}
		d.applyContainerDefaults(&container)
		if err := d.backend.UpsertContainer(container); err != nil {
			return registry.AssetResult{}, err
		}
		if err := d.connectExtraNetworks(in, spec.Networks, container); err != nil {
			return registry.AssetResult{}, err
		}
		if idx == 0 {
			primary = container
		}
	}
	if err := removeExtraContainers(d.backend, in, desired); err != nil {
		return registry.AssetResult{}, err
	}
	outputs := map[string]string{
		"id":      desired[0],
		"image":   primary.Image,
		"running": "true",
	}
	if port := firstContainerPort(primary.Ports); port > 0 {
		outputs["address"] = fmt.Sprintf("%s:%d", in.Asset.Name, port)
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ComputeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	containers, err := d.backend.ListContainersByAsset(in.PartitionName, in.IntentName, in.Asset.Name)
	if err != nil {
		return err
	}
	for _, container := range containers {
		if err := d.backend.DeleteContainer(container.Name); err != nil {
			return err
		}
	}
	return nil
}

func (d *TraefikRouteDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *TraefikRouteDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "Traefik route is managed by bootstrap"}, readyObservation("Traefik route metadata is ready"), nil
}

func (d *TraefikRouteDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeTraefikRoute(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if in.Store == nil {
		return changedDrift(in.Asset.Name, "Traefik route metadata pending"), nil
	}
	state, err := orchestratorcommon.LoadIntentState(ctx, in.Store, in.PartitionName, in.IntentName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return changedDrift(in.Asset.Name, "Traefik route metadata pending"), nil
		}
		return taskdomain.DriftReport{}, err
	}
	if strings.TrimSpace(state.Outputs[in.Asset.Name+".hostname"]) != strings.TrimSpace(spec.Hostname) ||
		strings.TrimSpace(state.Outputs[in.Asset.Name+".target"]) != strings.TrimSpace(spec.Target) ||
		strings.TrimSpace(state.Outputs[in.Asset.Name+".portName"]) != strings.TrimSpace(spec.PortName) {
		return changedDrift(in.Asset.Name, "Traefik route metadata differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "Traefik route metadata is in sync"), nil
}

func (d *TraefikRouteDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeTraefikRoute(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{
		"hostname": spec.Hostname,
		"target":   spec.Target,
		"portName": spec.PortName,
		"mode":     "bootstrap",
	}}, nil
}

func (d *TraefikRouteDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *LoadBalancerDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return err
	}
	for _, target := range spec.Targets {
		if _, err := d.backend.ListContainersByAsset(in.PartitionName, in.IntentName, target); err != nil {
			return err
		}
	}
	return nil
}

func (d *LoadBalancerDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readiness := readinessFromError(d.Check(ctx, in), "docker load balancer dependencies resolved")
	health, err := d.observeContainerHealth(ctx, loadBalancerName(in), "docker load balancer is running")
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *LoadBalancerDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	bootstrapConfig, err := d.loadBalancerBootstrap(in, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	name := loadBalancerName(in)
	actual, ok, err := d.backend.GetContainer(name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		return changedDrift(in.Asset.Name, "docker load balancer differs"), nil
	}
	desired := d.buildLoadBalancerContainer(in, spec, payload, bootstrapConfig)
	if drifted, reason := StructuralContainerDrift(desired, actual); drifted {
		return changedDrift(in.Asset.Name, "docker load balancer differs: "+reason), nil
	}
	return inSyncDrift(in.Asset.Name, "docker load balancer is in sync"), nil
}

func (d *LoadBalancerDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	network := networkName(in)
	if err := d.backend.EnsureNetwork(Network{Name: network, Labels: map[string]string{"guardian.cluster": in.Target.Cluster}}); err != nil {
		return registry.AssetResult{}, err
	}
	bootstrapConfig, err := d.loadBalancerBootstrap(in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	container := d.buildLoadBalancerContainer(in, spec, payload, bootstrapConfig)
	if err := d.backend.UpsertContainer(container); err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.connectExtraNetworks(in, spec.Networks, container); err != nil {
		return registry.AssetResult{}, err
	}
	outputs := map[string]string{"id": container.Name}
	if port := firstContainerPort(container.Ports); port > 0 {
		outputs["address"] = fmt.Sprintf("%s:%d", in.Asset.Name, port)
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *LoadBalancerDriver) buildLoadBalancerContainer(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec, payload containerPayload, bootstrapConfig string) Container {
	hash := loadBalancerContainerHash(in, spec, bootstrapConfig, payload)
	name := loadBalancerName(in)
	container := Container{
		Name:    name,
		Kind:    "LoadBalancer",
		Image:   loadBalancerContainerImage(),
		Hash:    hash,
		Labels:  driverutil.Labels("docker", in, hash),
		Network: networkName(in),
		Aliases: []string{in.Asset.Name, name},
		Env: map[string]string{
			"LB_BOOTSTRAP": bootstrapConfig,
		},
		Ports:   toLoadBalancerPorts(spec.Listeners),
		Running: true,
	}
	applyContainerPayload(&container, payload)
	d.applyContainerDefaults(&container)
	return container
}

func (d *LoadBalancerDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteContainer(loadBalancerName(in))
}

func (d *ObjectStoreDriver) Check(ctx context.Context, in registry.AssetInput) error {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return err
	}
	if spec.Endpoint != "" {
		return externalObjectStoreReady(ctx, spec)
	}
	return d.checkReferencedAssets(ctx, in)
}

func (d *ObjectStoreDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return nil, nil, err
	}
	if spec.Endpoint != "" {
		return observeExternalObjectStoreState(ctx, spec)
	}
	readiness := readinessFromError(d.checkReferencedAssets(ctx, in), "docker object store dependencies resolved")
	health, err := d.observeContainerHealth(ctx, objectStoreName(in), "docker object store is running")
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *ObjectStoreDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if spec.Endpoint != "" {
		return externalObjectStoreDiff(ctx, in, spec)
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffSingleContainer(ctx, in, objectStoreName(in), hashWithPayload(objectStoreHash(in), payload))
}

func (d *ObjectStoreDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if spec.Endpoint != "" {
		if err := externalObjectStoreReady(ctx, spec); err != nil {
			return registry.AssetResult{}, err
		}
		return registry.AssetResult{Outputs: map[string]string{
			"id":       in.Asset.Name,
			"endpoint": spec.Endpoint,
		}}, nil
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	network := networkName(in)
	if err := d.backend.EnsureNetwork(Network{Name: network, Labels: map[string]string{"guardian.cluster": in.Target.Cluster}}); err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(objectStoreHash(in), payload)
	container := Container{
		Name:         objectStoreName(in),
		Kind:         "ObjectStore",
		Image:        "minio/minio:latest",
		Hash:         hash,
		Labels:       driverutil.Labels("docker", in, hash),
		Network:      network,
		Aliases:      []string{in.Asset.Name, objectStoreName(in)},
		Command:      []string{"minio"},
		Args:         []string{"server", "/data", "--console-address=:9001"},
		Env:          map[string]string{"MINIO_ROOT_USER": "minio", "MINIO_ROOT_PASSWORD": "minio123"},
		Ports:        []PortBinding{{Name: "api", Protocol: "TCP", ContainerPort: 9000}, {Name: "console", Protocol: "TCP", ContainerPort: 9001}},
		VolumeMounts: objectStoreVolumeMounts(in, spec),
		ConfigMounts: objectStoreConfigMounts(in, spec),
		Running:      true,
	}
	applyContainerPayload(&container, payload)
	d.applyContainerDefaults(&container)
	if err := d.backend.UpsertContainer(container); err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.connectExtraNetworks(in, spec.Networks, container); err != nil {
		return registry.AssetResult{}, err
	}
	port := firstContainerPort(container.Ports)
	return registry.AssetResult{Outputs: map[string]string{
		"id":       container.Name,
		"endpoint": fmt.Sprintf("http://%s:%d", in.Asset.Name, port),
	}}, nil
}

func (d *ObjectStoreDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeObjectStore(in)
	if err != nil {
		return err
	}
	if spec.Endpoint != "" {
		return nil
	}
	return d.backend.DeleteContainer(objectStoreName(in))
}

func (d *SQLDatabaseDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return d.checkReferencedAssets(ctx, in)
}

func (d *SQLDatabaseDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readiness := readinessFromError(d.Check(ctx, in), "docker database dependencies resolved")
	health, err := d.observeContainerHealth(ctx, sqlDatabaseName(in), "docker database is running")
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *SQLDatabaseDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffSingleContainer(ctx, in, sqlDatabaseName(in), hashWithPayload(sqlDatabaseHash(in), payload))
}

func (d *SQLDatabaseDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	engine := strings.ToLower(spec.Engine)
	image, port, env := sqlDatabaseImagePortEnv(spec)
	network := networkName(in)
	if err := d.backend.EnsureNetwork(Network{Name: network, Labels: map[string]string{"guardian.cluster": in.Target.Cluster}}); err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(sqlDatabaseHash(in), payload)
	container := Container{
		Name:         sqlDatabaseName(in),
		Kind:         "SQLDatabase",
		Image:        image,
		Hash:         hash,
		Labels:       driverutil.Labels("docker", in, hash),
		Network:      network,
		Aliases:      []string{in.Asset.Name, sqlDatabaseName(in)},
		Env:          env,
		Ports:        []PortBinding{{Name: "db", Protocol: "TCP", ContainerPort: port}},
		VolumeMounts: sqlDatabaseVolumeMounts(in, spec, engine),
		ConfigMounts: sqlDatabaseConfigMounts(in, spec),
		Running:      true,
	}
	applyContainerPayload(&container, payload)
	d.applyContainerDefaults(&container)
	if err := d.backend.UpsertContainer(container); err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.connectExtraNetworks(in, spec.Networks, container); err != nil {
		return registry.AssetResult{}, err
	}
	resolvedPort := firstContainerPort(container.Ports)
	return registry.AssetResult{Outputs: map[string]string{
		"id":     container.Name,
		"engine": engine,
		"url":    fmt.Sprintf("%s://%s:%d/%s", engine, in.Asset.Name, resolvedPort, defaultDatabaseName(spec, in.Asset.Name)),
	}}, nil
}

func (d *SQLDatabaseDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteContainer(sqlDatabaseName(in))
}

func (d *ObservabilityDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return d.checkReferencedAssets(ctx, in)
}

func (d *ObservabilityDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readiness := readinessFromError(d.Check(ctx, in), "docker observability dependencies resolved")
	health, err := d.observeContainerHealth(ctx, observabilityName(in), "docker observability collector is running")
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *ObservabilityDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffSingleContainer(ctx, in, observabilityName(in), hashWithPayload(observabilityHash(in), payload))
}

func (d *ObservabilityDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeObservability(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadContainerPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	network := networkName(in)
	if err := d.backend.EnsureNetwork(Network{Name: network, Labels: map[string]string{"guardian.cluster": in.Target.Cluster}}); err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(observabilityHash(in), payload)
	port := observabilityPort(spec)
	container := Container{
		Name:         observabilityName(in),
		Kind:         "Observability",
		Image:        "otel/opentelemetry-collector-contrib:0.102.1",
		Hash:         hash,
		Labels:       driverutil.Labels("docker", in, hash),
		Network:      network,
		Aliases:      []string{in.Asset.Name, observabilityName(in)},
		Command:      []string{"otelcol-contrib"},
		Args:         []string{"--config=/etc/otelcol/config.yaml"},
		Ports:        []PortBinding{{Name: "otlp", Protocol: "TCP", ContainerPort: port}},
		ConfigMounts: observabilityConfigMounts(in, spec),
		InlineFiles:  observabilityInlineFiles(spec),
		VolumeMounts: observabilityVolumeMounts(in, spec),
		Running:      true,
	}
	applyContainerPayload(&container, payload)
	d.applyContainerDefaults(&container)
	if err := d.backend.UpsertContainer(container); err != nil {
		return registry.AssetResult{}, err
	}
	if err := d.connectExtraNetworks(in, spec.Networks, container); err != nil {
		return registry.AssetResult{}, err
	}
	resolvedPort := firstContainerPort(container.Ports)
	return registry.AssetResult{Outputs: map[string]string{
		"id":       container.Name,
		"endpoint": fmt.Sprintf("%s:%d", in.Asset.Name, resolvedPort),
	}}, nil
}

func (d *ObservabilityDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteContainer(observabilityName(in))
}

func (d *baseDriver) checkReferencedAssets(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, dep := range in.Asset.DependsOn {
		asset, ok := in.Assets[dep]
		if !ok {
			continue
		}
		switch asset.Type {
		case "Volume":
			if _, _, err := d.backend.GetVolume(volumeName(in, dep)); err != nil {
				return err
			}
		case "Config":
			if _, _, err := d.backend.GetConfig(configName(in, dep)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *baseDriver) diffSingleContainer(ctx context.Context, in registry.AssetInput, name, hash string) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	container, ok, err := d.backend.GetContainer(name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || container.Hash != hash {
		return changedDrift(in.Asset.Name, "docker container differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "docker resource is in sync"), nil
}

func (d *baseDriver) observeContainerHealth(ctx context.Context, name, healthySummary string) (*taskdomain.HealthObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	container, ok, err := d.backend.GetContainer(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("docker container %s is missing", name)}, nil
	}
	if !container.Running {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("docker container %s is not running", name)}, nil
	}
	if strings.TrimSpace(healthySummary) == "" {
		healthySummary = fmt.Sprintf("docker container %s is running", name)
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: healthySummary}, nil
}

func readinessFromError(err error, readySummary string) *taskdomain.ApplyReadiness {
	if err != nil {
		return &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessBlocked, Summary: err.Error()}
	}
	return readyObservation(readySummary)
}

func readyObservation(summary string) *taskdomain.ApplyReadiness {
	if strings.TrimSpace(summary) == "" {
		summary = "dependencies resolved"
	}
	return &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: summary}
}

func (d *baseDriver) applyContainerDefaults(container *Container) {
	if container == nil || len(d.defaultExtraHosts) == 0 {
		return
	}
	if container.ExtraHosts == nil {
		container.ExtraHosts = map[string]string{}
	}
	for host, address := range d.defaultExtraHosts {
		host = strings.TrimSpace(host)
		address = strings.TrimSpace(address)
		if host == "" || address == "" {
			continue
		}
		if _, exists := container.ExtraHosts[host]; !exists {
			container.ExtraHosts[host] = address
		}
	}
}

func (d *baseDriver) connectExtraNetworks(in registry.AssetInput, networks []string, container Container) error {
	for _, netAsset := range networks {
		netName := explicitNetworkName(in, netAsset)
		if err := d.backend.EnsureNetwork(Network{
			Name:   netName,
			Driver: "bridge",
			Labels: map[string]string{"guardian.cluster": in.Target.Cluster},
		}); err != nil {
			return fmt.Errorf("ensure network %s: %w", netName, err)
		}
		if err := d.backend.ConnectNetwork(netName, container.Name, container.Aliases); err != nil {
			return fmt.Errorf("connect network %s to %s: %w", netName, container.Name, err)
		}
	}
	return nil
}

func decodeVolume(in registry.AssetInput) (*assetdefs.VolumeSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.VolumeSpec)
	if !ok {
		return nil, fmt.Errorf("expected VolumeSpec, got %T", typed)
	}
	return spec, nil
}

func decodeConfig(in registry.AssetInput) (*assetdefs.ConfigSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ConfigSpec)
	if !ok {
		return nil, fmt.Errorf("expected ConfigSpec, got %T", typed)
	}
	return spec, nil
}

func decodeNetwork(in registry.AssetInput) (*assetdefs.NetworkSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.NetworkSpec)
	if !ok {
		return nil, fmt.Errorf("expected NetworkSpec, got %T", typed)
	}
	return spec, nil
}

func decodeCompute(in registry.AssetInput) (*assetdefs.ComputeSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ComputeSpec)
	if !ok {
		return nil, fmt.Errorf("expected ComputeSpec, got %T", typed)
	}
	return spec, nil
}

func decodeLoadBalancer(in registry.AssetInput) (*assetdefs.LoadBalancerSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.LoadBalancerSpec)
	if !ok {
		return nil, fmt.Errorf("expected LoadBalancerSpec, got %T", typed)
	}
	return spec, nil
}

func decodeTraefikRoute(in registry.AssetInput) (*assetdefs.TraefikRouteSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.TraefikRouteSpec)
	if !ok {
		return nil, fmt.Errorf("expected TraefikRouteSpec, got %T", typed)
	}
	return spec, nil
}

func decodeObjectStore(in registry.AssetInput) (*assetdefs.ObjectStoreSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ObjectStoreSpec)
	if !ok {
		return nil, fmt.Errorf("expected ObjectStoreSpec, got %T", typed)
	}
	return spec, nil
}

func decodeSQLDatabase(in registry.AssetInput) (*assetdefs.SQLDatabaseSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.SQLDatabaseSpec)
	if !ok {
		return nil, fmt.Errorf("expected SQLDatabaseSpec, got %T", typed)
	}
	return spec, nil
}

func decodeObservability(in registry.AssetInput) (*assetdefs.ObservabilitySpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ObservabilitySpec)
	if !ok {
		return nil, fmt.Errorf("expected ObservabilitySpec, got %T", typed)
	}
	return spec, nil
}

func computeHash(in registry.AssetInput, spec *assetdefs.ComputeSpec) string {
	refs := make([]string, 0, len(spec.VolumeMounts)+len(spec.ConfigMounts))
	for _, mount := range spec.VolumeMounts {
		refs = append(refs, mount.Volume)
	}
	for _, mount := range spec.ConfigMounts {
		refs = append(refs, mount.Config)
	}
	return driverutil.CompositeHash(in, refs...)
}

func loadBalancerHash(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) string {
	refs := append([]string{}, spec.Targets...)
	if spec.Config != "" {
		refs = append(refs, spec.Config)
	}
	return driverutil.CompositeHash(in, refs...)
}

func loadBalancerContainerHash(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec, configContent string, payload containerPayload) string {
	base := digest.MustNormalizedHash(struct {
		Base   string
		Config string
	}{
		Base:   loadBalancerHash(in, spec),
		Config: configContent,
	})
	return hashWithPayload(base, payload)
}

func objectStoreHash(in registry.AssetInput) string {
	return driverutil.CompositeHash(in)
}

func externalObjectStoreDiff(ctx context.Context, in registry.AssetInput, spec *assetdefs.ObjectStoreSpec) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	if in.Store == nil {
		return changedDrift(in.Asset.Name, "external object store reference pending"), nil
	}
	state, err := orchestratorcommon.LoadIntentState(ctx, in.Store, in.PartitionName, in.IntentName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return changedDrift(in.Asset.Name, "external object store reference pending"), nil
		}
		return taskdomain.DriftReport{}, err
	}
	want := strings.TrimSpace(spec.Endpoint)
	got := strings.TrimSpace(state.Outputs[in.Asset.Name+".endpoint"])
	if got != want {
		return changedDrift(in.Asset.Name, "external object store endpoint differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "external object store reference"), nil
}

func observeExternalObjectStoreState(ctx context.Context, spec *assetdefs.ObjectStoreSpec) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	readiness := readyObservation("external object store reference resolved")
	if spec == nil || strings.TrimSpace(spec.Endpoint) == "" {
		return nil, readiness, nil
	}
	if strings.TrimSpace(spec.AccessKeyID) == "" || strings.TrimSpace(spec.SecretAccessKey) == "" {
		return nil, readiness, nil
	}
	if err := externalObjectStoreReady(ctx, spec); err != nil {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: err.Error()}, readiness, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "external object store is reachable"}, readiness, nil
}

func externalObjectStoreReady(ctx context.Context, spec *assetdefs.ObjectStoreSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if spec == nil {
		return fmt.Errorf("external object store spec is required")
	}
	if strings.TrimSpace(spec.Endpoint) == "" {
		return nil
	}
	if strings.TrimSpace(spec.AccessKeyID) == "" || strings.TrimSpace(spec.SecretAccessKey) == "" {
		return nil
	}
	region := strings.TrimSpace(spec.Region)
	if region == "" {
		region = "us-east-1"
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(spec.AccessKeyID),
			strings.TrimSpace(spec.SecretAccessKey),
			"",
		)),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("load external object store client config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(opts *s3.Options) {
		opts.UsePathStyle = driverutil.BoolValue(spec.UsePathStyle)
		opts.EndpointResolver = s3.EndpointResolverFromURL(strings.TrimSpace(spec.Endpoint))
	})
	for _, bucket := range spec.Buckets {
		name := strings.TrimSpace(bucket)
		if name == "" {
			continue
		}
		if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &name}); err != nil {
			return fmt.Errorf("external object store bucket %q not ready: %w", name, err)
		}
	}
	return nil
}

func sqlDatabaseHash(in registry.AssetInput) string {
	return driverutil.CompositeHash(in)
}

func observabilityHash(in registry.AssetInput) string {
	return driverutil.CompositeHash(in)
}

func buildComputeContainer(in registry.AssetInput, spec *assetdefs.ComputeSpec, name string, idx int, hash, network string, env map[string]string, payload containerPayload) (Container, error) {
	container := Container{
		Name:         name,
		Kind:         "Compute",
		Image:        spec.Image,
		Hash:         hash,
		Labels:       driverutil.Labels("docker", in, hash),
		Network:      network,
		Aliases:      []string{in.Asset.Name, name},
		Command:      []string(spec.Command),
		Args:         []string(spec.Args),
		Env:          env,
		Ports:        toComputePorts(spec.Ports),
		Privileged:   driverutil.BoolValue(spec.Privileged),
		Capabilities: append([]string(nil), spec.Capabilities...),
		ShmSize:      spec.ShmSize,
		GPUs:         spec.GPUs,
		Running:      true,
	}
	if r := spec.Resources; r != nil {
		container.CPULimit = r.Limits.CPU
		container.MemoryLimit = r.Limits.Memory
		container.MemoryReservation = r.Requests.Memory
	}
	if idx > 0 {
		container.Aliases = []string{name}
	}
	for _, mount := range spec.VolumeMounts {
		container.VolumeMounts = append(container.VolumeMounts, VolumeMount{
			Source:   volumeName(in, mount.Volume),
			Target:   mount.Path,
			ReadOnly: mount.ReadOnly,
		})
	}
	for _, mount := range spec.ConfigMounts {
		_, typed, err := driverutil.DecodeNamedAsset(in, mount.Config)
		if err != nil {
			return Container{}, err
		}
		configSpec := typed.(*assetdefs.ConfigSpec)
		configMount := ConfigMount{
			Config:     configName(in, mount.Config),
			TargetPath: mount.Path,
			ReadOnly:   mount.ReadOnly,
		}
		if fileName, _, ok := driverutil.SingleConfigFile(configSpec); ok {
			configMount.SourcePath = fileName
		}
		container.ConfigMounts = append(container.ConfigMounts, configMount)
	}
	for _, mount := range spec.HostBindMounts {
		container.HostBindMounts = append(container.HostBindMounts, HostBindMount{
			Source:   mount.Source,
			Target:   mount.Target,
			ReadOnly: mount.ReadOnly,
		})
	}
	applyContainerPayload(&container, payload)
	return container, nil
}

func computeContainerNames(in registry.AssetInput, spec *assetdefs.ComputeSpec) []string {
	replicas := driverutil.IntValue(spec.Replicas, 1)
	if replicas < 1 {
		replicas = 1
	}
	names := make([]string, 0, replicas)
	for i := 0; i < replicas; i++ {
		names = append(names, driverutil.ResourceName("docker-ct", in.Target, in.PartitionName, in.IntentName, in.Asset.Name, strconv.Itoa(i)))
	}
	return names
}

func objectStoreVolumeMounts(in registry.AssetInput, spec *assetdefs.ObjectStoreSpec) []VolumeMount {
	if spec.Volume == "" {
		return nil
	}
	return []VolumeMount{{Source: volumeName(in, spec.Volume), Target: "/data"}}
}

func objectStoreConfigMounts(in registry.AssetInput, spec *assetdefs.ObjectStoreSpec) []ConfigMount {
	if spec.Config == "" {
		return nil
	}
	return []ConfigMount{{Config: configName(in, spec.Config), TargetPath: "/etc/minio", ReadOnly: true}}
}

func sqlDatabaseVolumeMounts(in registry.AssetInput, spec *assetdefs.SQLDatabaseSpec, engine string) []VolumeMount {
	if spec.Volume == "" {
		return nil
	}
	target := "/var/lib/data"
	switch engine {
	case "postgres", "postgresql":
		target = "/var/lib/postgresql/data"
	case "mysql", "mariadb":
		target = "/var/lib/mysql"
	}
	return []VolumeMount{{Source: volumeName(in, spec.Volume), Target: target}}
}

func sqlDatabaseConfigMounts(in registry.AssetInput, spec *assetdefs.SQLDatabaseSpec) []ConfigMount {
	if spec.Config == "" {
		return nil
	}
	return []ConfigMount{{Config: configName(in, spec.Config), TargetPath: "/etc/guardian-db", ReadOnly: true}}
}

func observabilityConfigMounts(in registry.AssetInput, spec *assetdefs.ObservabilitySpec) []ConfigMount {
	if spec.Config == "" {
		return nil
	}
	return []ConfigMount{{Config: configName(in, spec.Config), TargetPath: "/etc/otelcol/config.yaml", ReadOnly: true, SourcePath: firstConfigPath(in, spec.Config)}}
}

func observabilityInlineFiles(spec *assetdefs.ObservabilitySpec) map[string]string {
	if spec.Config != "" {
		return nil
	}
	endpoint := spec.Endpoint
	if endpoint == "" {
		endpoint = "0.0.0.0:4317"
	}
	exporters := "logging: {}"
	if len(spec.Exporters) > 0 {
		lines := make([]string, 0, len(spec.Exporters))
		for _, exporter := range spec.Exporters {
			lines = append(lines, fmt.Sprintf("  %s: {}", exporter))
		}
		exporters = strings.Join(lines, "\n")
	}
	return map[string]string{
		"config.yaml": fmt.Sprintf("receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: %s\nexporters:\n%s\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [%s]\n", endpoint, exporters, firstExporter(spec)),
	}
}

func observabilityVolumeMounts(in registry.AssetInput, spec *assetdefs.ObservabilitySpec) []VolumeMount {
	if spec.Volume == "" {
		return nil
	}
	return []VolumeMount{{Source: volumeName(in, spec.Volume), Target: "/var/lib/otelcol"}}
}

func sqlDatabaseImagePortEnv(spec *assetdefs.SQLDatabaseSpec) (string, int, map[string]string) {
	engine := strings.ToLower(strings.TrimSpace(spec.Engine))
	version := strings.TrimSpace(spec.Version)
	dbName := defaultDatabaseName(spec, "app")
	user := spec.User
	if user == "" {
		user = "guardian"
	}
	switch engine {
	case "mysql":
		if version == "" {
			version = "8"
		}
		return "mysql:" + version, defaultPort(spec.Port, 3306), map[string]string{
			"MYSQL_DATABASE":             dbName,
			"MYSQL_USER":                 user,
			"MYSQL_ALLOW_EMPTY_PASSWORD": "yes",
		}
	case "mariadb":
		if version == "" {
			version = "11"
		}
		return "mariadb:" + version, defaultPort(spec.Port, 3306), map[string]string{
			"MARIADB_DATABASE":                  dbName,
			"MARIADB_USER":                      user,
			"MARIADB_ALLOW_EMPTY_ROOT_PASSWORD": "yes",
		}
	default:
		if version == "" {
			version = "16"
		}
		return "postgres:" + version, defaultPort(spec.Port, 5432), map[string]string{
			"POSTGRES_DB":               dbName,
			"POSTGRES_USER":             user,
			"POSTGRES_HOST_AUTH_METHOD": "trust",
		}
	}
}

func defaultDatabaseName(spec *assetdefs.SQLDatabaseSpec, fallback string) string {
	if strings.TrimSpace(spec.Database) != "" {
		return spec.Database
	}
	return fallback
}

func defaultPort(port *int, fallback int) int {
	if port != nil && *port > 0 {
		return *port
	}
	return fallback
}

func toComputePorts(ports []assetdefs.PortSpec) []PortBinding {
	out := make([]PortBinding, 0, len(ports))
	for _, port := range ports {
		containerPort := 0
		if port.ContainerPort != nil {
			containerPort = *port.ContainerPort
		} else if port.Port != nil {
			containerPort = *port.Port
		}
		hostPort := 0
		if strings.TrimSpace(port.DynamicHostname) == "" && port.HostPort != nil {
			hostPort = *port.HostPort
		}
		out = append(out, PortBinding{Name: port.Name, Protocol: firstNonEmpty(port.Protocol, "TCP"), ContainerPort: containerPort, HostPort: hostPort})
	}
	return out
}

func toLoadBalancerPorts(listeners []assetdefs.ListenerSpec) []PortBinding {
	out := make([]PortBinding, 0, len(listeners))
	for _, listener := range listeners {
		if listener.Port == nil {
			continue
		}
		out = append(out, PortBinding{
			Name:          listener.Name,
			Protocol:      firstNonEmpty(listener.Protocol, "TCP"),
			ContainerPort: *listener.Port,
			HostPort:      *listener.Port,
		})
	}
	return out
}

func loadBalancerContainerImage() string {
	if override := strings.TrimSpace(os.Getenv("GUARDIAN_LB_IMAGE")); override != "" {
		return override
	}
	return "lb:latest"
}

func firstContainerPort(ports []PortBinding) int {
	for _, port := range ports {
		if port.ContainerPort > 0 {
			return port.ContainerPort
		}
		if port.HostPort > 0 {
			return port.HostPort
		}
	}
	return 0
}

func (d *LoadBalancerDriver) loadBalancerConfig(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) (string, error) {
	if spec.Config != "" {
		_, typed, err := driverutil.DecodeNamedAsset(in, spec.Config)
		if err != nil {
			return "", err
		}
		configSpec := typed.(*assetdefs.ConfigSpec)
		if _, content, ok := driverutil.SingleConfigFile(configSpec); ok {
			return content, nil
		}
		files := driverutil.ConfigFiles(configSpec)
		keys := make([]string, 0, len(files))
		for key := range files {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			return files[keys[0]], nil
		}
	}
	return generateHAProxyConfig(in, spec)
}

func (d *LoadBalancerDriver) loadBalancerBootstrap(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) (string, error) {
	entries := make([]string, 0, len(spec.Listeners))
	for _, listener := range spec.Listeners {
		if listener.Port == nil {
			continue
		}
		backends := make([]string, 0, len(spec.Targets))
		for _, target := range spec.Targets {
			_, typed, err := driverutil.DecodeNamedAsset(in, target)
			if err != nil {
				return "", err
			}
			compute := typed.(*assetdefs.ComputeSpec)
			replicas := driverutil.IntValue(compute.Replicas, 1)
			port := matchComputePort(compute, *listener.Port)
			for replica := 0; replica < replicas; replica++ {
				serverName := driverutil.ResourceName("docker-ct", in.Target, in.PartitionName, in.IntentName, target, strconv.Itoa(replica))
				backends = append(backends, fmt.Sprintf("%s:%d", serverName, port))
			}
		}
		if len(backends) == 0 {
			continue
		}
		entries = append(entries, driverutil.BuildBootstrapEntry(listener, backends))
	}
	return strings.Join(entries, ";"), nil
}

func generateHAProxyConfig(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) (string, error) {
	var b strings.Builder
	b.WriteString("global\n  daemon\n")
	b.WriteString("defaults\n  mode tcp\n  timeout connect 5s\n  timeout client 30s\n  timeout server 30s\n")
	b.WriteString("resolvers docker\n  nameserver dns 127.0.0.11:53\n  resolve_retries 3\n  timeout resolve 1s\n  timeout retry 1s\n  hold valid 10s\n")
	for idx, listener := range spec.Listeners {
		if listener.Port == nil {
			continue
		}
		frontend := listener.Name
		if frontend == "" {
			frontend = fmt.Sprintf("listener-%d", idx)
		}
		backendName := frontend + "-backend"
		b.WriteString(fmt.Sprintf("frontend %s\n  bind *:%d\n  default_backend %s\n", frontend, *listener.Port, backendName))
		b.WriteString(fmt.Sprintf("backend %s\n", backendName))
		for _, target := range spec.Targets {
			_, typed, err := driverutil.DecodeNamedAsset(in, target)
			if err != nil {
				return "", err
			}
			compute := typed.(*assetdefs.ComputeSpec)
			replicas := driverutil.IntValue(compute.Replicas, 1)
			port := matchComputePort(compute, *listener.Port)
			b.WriteString("  default-server init-addr last,libc,none resolvers docker resolve-prefer ipv4\n")
			for replica := 0; replica < replicas; replica++ {
				serverName := driverutil.ResourceName("docker-ct", in.Target, in.PartitionName, in.IntentName, target, strconv.Itoa(replica))
				b.WriteString(fmt.Sprintf("  server %s %s:%d check\n", serverName, serverName, port))
			}
		}
	}
	return b.String(), nil
}

func matchComputePort(spec *assetdefs.ComputeSpec, prefer int) int {
	for _, port := range spec.Ports {
		if port.ContainerPort != nil && (*port.ContainerPort == prefer || prefer == 0) {
			return *port.ContainerPort
		}
		if port.Port != nil && (*port.Port == prefer || prefer == 0) {
			return *port.Port
		}
	}
	return driverutil.FirstPort(spec.Ports)
}

func volumeName(in registry.AssetInput, assetName string) string {
	return driverutil.ResourceName("docker-vol", in.Target, in.PartitionName, in.IntentName, assetName)
}

func configName(in registry.AssetInput, assetName string) string {
	return driverutil.ResourceName("docker-cfg", in.Target, in.PartitionName, in.IntentName, assetName)
}

func networkName(in registry.AssetInput) string {
	return driverutil.ResourceName("docker-net", in.Target, "", "", "")
}

func explicitNetworkName(in registry.AssetInput, assetName string) string {
	return driverutil.ResourceName("docker-net", in.Target, "", "", assetName)
}

func loadBalancerName(in registry.AssetInput) string {
	return driverutil.ResourceName("docker-lb", in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func objectStoreName(in registry.AssetInput) string {
	return driverutil.ResourceName("docker-obj", in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func sqlDatabaseName(in registry.AssetInput) string {
	return driverutil.ResourceName("docker-db", in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func observabilityName(in registry.AssetInput) string {
	return driverutil.ResourceName("docker-obs", in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func removeExtraContainers(backend BackendAPI, in registry.AssetInput, desired []string) error {
	want := make(map[string]struct{}, len(desired))
	for _, name := range desired {
		want[name] = struct{}{}
	}
	containers, err := backend.ListContainersByAsset(in.PartitionName, in.IntentName, in.Asset.Name)
	if err != nil {
		return err
	}
	for _, container := range containers {
		if _, ok := want[container.Name]; ok {
			continue
		}
		if err := backend.DeleteContainer(container.Name); err != nil {
			return err
		}
	}
	return nil
}

func changedDrift(asset, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{Status: "Changed", Summary: summary, ChangedAssets: []string{asset}}
}

func inSyncDrift(asset, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{Status: "InSync", Summary: summary, ChangedAssets: []string{}}
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func equalStringMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func observabilityPort(spec *assetdefs.ObservabilitySpec) int {
	if spec.Endpoint == "" {
		return 4317
	}
	parts := strings.Split(spec.Endpoint, ":")
	if len(parts) == 0 {
		return 4317
	}
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || port <= 0 {
		return 4317
	}
	return port
}

func firstExporter(spec *assetdefs.ObservabilitySpec) string {
	if len(spec.Exporters) == 0 {
		return "logging"
	}
	return spec.Exporters[0]
}

func firstConfigPath(in registry.AssetInput, assetName string) string {
	_, typed, err := driverutil.DecodeNamedAsset(in, assetName)
	if err != nil {
		return ""
	}
	file, _, ok := driverutil.SingleConfigFile(typed.(*assetdefs.ConfigSpec))
	if !ok {
		return ""
	}
	return file
}
