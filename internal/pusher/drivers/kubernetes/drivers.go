package kubernetesdriver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	orchestratorcommon "github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
)

type baseDriver struct {
	backend  BackendAPI
	resolver secrets.Resolver
}

type VolumeDriver struct{ baseDriver }
type ConfigDriver struct{ baseDriver }
type ComputeDriver struct{ baseDriver }
type TraefikRouteDriver struct{ baseDriver }
type LoadBalancerDriver struct{ baseDriver }
type ObjectStoreDriver struct{ baseDriver }
type SQLDatabaseDriver struct {
	baseDriver
	typeName string
}
type ObservabilityDriver struct{ baseDriver }

func Register(reg *registry.Registry, backend BackendAPI, resolver secrets.Resolver) {
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
	reg.Register(&VolumeDriver{base})
	reg.Register(&ConfigDriver{base})
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
func (d *ComputeDriver) Type() string                        { return "Compute" }
func (d *TraefikRouteDriver) Type() string                   { return "TraefikRoute" }
func (d *LoadBalancerDriver) Type() string                   { return "LoadBalancer" }
func (d *ObjectStoreDriver) Type() string                    { return "ObjectStore" }
func (d *SQLDatabaseDriver) Type() string                    { return d.typeName }
func (d *ObservabilityDriver) Type() string                  { return "Observability" }
func (d *VolumeDriver) Validate(map[string]any) error        { return nil }
func (d *ConfigDriver) Validate(map[string]any) error        { return nil }
func (d *ComputeDriver) Validate(map[string]any) error       { return nil }
func (d *TraefikRouteDriver) Validate(map[string]any) error  { return nil }
func (d *LoadBalancerDriver) Validate(map[string]any) error  { return nil }
func (d *ObjectStoreDriver) Validate(map[string]any) error   { return nil }
func (d *SQLDatabaseDriver) Validate(map[string]any) error   { return nil }
func (d *ObservabilityDriver) Validate(map[string]any) error { return nil }

func (d *VolumeDriver) Check(ctx context.Context, in registry.AssetInput) error { return ctx.Err() }

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
	ephemeral := driverutil.BoolValue(spec.Ephemeral)
	if payload.Ephemeral != nil {
		ephemeral = *payload.Ephemeral
	}
	if ephemeral {
		return inSyncDrift(in.Asset.Name, "ephemeral volume is in sync"), nil
	}
	name := claimName(in, in.Asset.Name)
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	claim, ok, err := d.backend.GetClaim(namespace(in), name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || claim.Hash != hash || claim.Size != firstNonEmpty(payload.Size, firstNonEmpty(spec.Size, "1Gi")) || claim.AccessMode != firstNonEmpty(payload.AccessMode, firstNonEmpty(spec.AccessMode, "ReadWriteOnce")) || claim.StorageClass != firstNonEmpty(payload.StorageClass, spec.Class) {
		return changedDrift(in.Asset.Name, "kubernetes volume differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "kubernetes volume is in sync"), nil
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
	ephemeral := driverutil.BoolValue(spec.Ephemeral)
	if payload.Ephemeral != nil {
		ephemeral = *payload.Ephemeral
	}
	if ephemeral {
		return registry.AssetResult{Outputs: map[string]string{"kind": "emptyDir"}}, nil
	}
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	name := claimName(in, in.Asset.Name)
	if err := d.backend.UpsertClaim(PersistentVolumeClaim{
		Namespace:    namespace(in),
		Name:         name,
		Hash:         hash,
		Labels:       driverutil.Labels("kubernetes", in, hash),
		Size:         firstNonEmpty(payload.Size, firstNonEmpty(spec.Size, "1Gi")),
		AccessMode:   firstNonEmpty(payload.AccessMode, firstNonEmpty(spec.AccessMode, "ReadWriteOnce")),
		StorageClass: firstNonEmpty(payload.StorageClass, spec.Class),
	}); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{"name": name}}, nil
}

func (d *VolumeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteClaim(namespace(in), claimName(in, in.Asset.Name))
}

func (d *ConfigDriver) Check(ctx context.Context, in registry.AssetInput) error { return ctx.Err() }

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
	hash := hashWithPayload(driverutil.AssetHash(in), payload)
	name := configMapName(in, in.Asset.Name)
	data := driverutil.ConfigFiles(spec)
	if len(payload.Data) > 0 {
		data = cloneStringMap(payload.Data)
	}
	cm, ok, err := d.backend.GetConfigMap(namespace(in), name)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || cm.Hash != hash || !equalStringMaps(cm.Data, data) {
		return changedDrift(in.Asset.Name, "kubernetes config differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "kubernetes config is in sync"), nil
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
	name := configMapName(in, in.Asset.Name)
	data := driverutil.ConfigFiles(spec)
	if len(payload.Data) > 0 {
		data = cloneStringMap(payload.Data)
	}
	if err := d.backend.UpsertConfigMap(ConfigMap{
		Namespace: namespace(in),
		Name:      name,
		Hash:      hash,
		Labels:    driverutil.Labels("kubernetes", in, hash),
		Data:      data,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	return registry.AssetResult{Outputs: map[string]string{"name": name}}, nil
}

func (d *ConfigDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteConfigMap(namespace(in), configMapName(in, in.Asset.Name))
}

func (d *ComputeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := d.checkReferences(ctx, in); err != nil {
		return err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return err
	}
	deployment, ok, err := d.backend.GetDeployment(namespace(in), computeDeploymentName(in, spec))
	if err != nil {
		return err
	}
	if ok && deployment.CrashLoopBackOff {
		reason := deployment.PodFailureReason
		if reason == "" {
			reason = "CrashLoopBackOff"
		}
		return fmt.Errorf("deployment %s has pods in %s", deployment.Name, reason)
	}
	return nil
}

func (d *ComputeDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	readiness, err := d.observeApplyReadiness(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return nil, nil, err
	}
	health, err := d.observeComputeHealth(ctx, in, len(spec.Ports) > 0)
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *ComputeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	hash := hashWithPayload(computeHash(in, spec), payload)
	deploymentName := computeDeploymentName(in, spec)
	serviceName := computeServiceName(in, spec)
	deployment, ok, err := d.backend.GetDeployment(namespace(in), deploymentName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if spec.ObserveExisting {
		if !ok {
			return changedDrift(in.Asset.Name, "kubernetes deployment differs"), nil
		}
		if len(spec.Ports) > 0 {
			service, serviceOK, err := d.backend.GetService(namespace(in), serviceName)
			if err != nil {
				return taskdomain.DriftReport{}, err
			}
			if !serviceOK {
				return changedDrift(in.Asset.Name, "kubernetes service differs"), nil
			}
			_ = service
		}
		return inSyncDrift(in.Asset.Name, "kubernetes compute is in sync"), nil
	}
	replicas := max(1, driverutil.IntValue(spec.Replicas, 1))
	if payload.Replicas != nil && *payload.Replicas > 0 {
		replicas = *payload.Replicas
	}
	if !ok || deployment.Hash != hash || deployment.Replicas != replicas {
		return changedDrift(in.Asset.Name, "kubernetes deployment differs"), nil
	}
	if len(spec.Ports) > 0 {
		service, ok, err := d.backend.GetService(namespace(in), serviceName)
		if err != nil {
			return taskdomain.DriftReport{}, err
		}
		if !ok || service.Hash != hash {
			return changedDrift(in.Asset.Name, "kubernetes service differs"), nil
		}
	}
	return inSyncDrift(in.Asset.Name, "kubernetes compute is in sync"), nil
}

func (d *ComputeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if spec.ObserveExisting {
		deploymentName := computeDeploymentName(in, spec)
		deployment, ok, err := d.backend.GetDeployment(namespace(in), deploymentName)
		if err != nil {
			return registry.AssetResult{}, err
		}
		if !ok {
			return registry.AssetResult{}, fmt.Errorf("observed deployment %s is missing", deploymentName)
		}
		outputs := map[string]string{"id": deploymentName, "image": deployment.Container.Image}
		if len(spec.Ports) > 0 {
			svcName := computeServiceName(in, spec)
			svc, ok, err := d.backend.GetService(namespace(in), svcName)
			if err != nil {
				return registry.AssetResult{}, err
			}
			if !ok {
				return registry.AssetResult{}, fmt.Errorf("observed service %s is missing", svcName)
			}
			if port := firstServicePort(svc.Ports); port > 0 {
				outputs["address"] = fmt.Sprintf("%s.%s.svc.cluster.local:%d", svc.Name, namespace(in), port)
			}
		}
		return registry.AssetResult{Outputs: outputs}, nil
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(computeHash(in, spec), payload)
	env, err := driverutil.ResolveEnv(ctx, d.resolver, spec.Env)
	if err != nil {
		return registry.AssetResult{}, err
	}
	container, err := buildComputeContainer(ctx, in, d.resolver, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	container.Env = env
	container.Image = spec.Image
	deployment := Deployment{
		Namespace:         namespace(in),
		Name:              computeDeploymentName(in, spec),
		Kind:              "Compute",
		Hash:              hash,
		Labels:            driverutil.Labels("kubernetes", in, hash),
		Replicas:          max(1, driverutil.IntValue(spec.Replicas, 1)),
		ReadyReplicas:     max(1, driverutil.IntValue(spec.Replicas, 1)),
		AvailableReplicas: max(1, driverutil.IntValue(spec.Replicas, 1)),
		Container:         container,
	}
	applyWorkloadPayload(&deployment, payload)
	markDeploymentReady(&deployment)
	if err := d.backend.UpsertDeployment(deployment); err != nil {
		return registry.AssetResult{}, err
	}
	outputs := map[string]string{"id": computeDeploymentName(in, spec), "image": deployment.Container.Image}
	ports := toServicePorts(spec.Ports)
	if len(payload.ServicePorts) > 0 {
		ports = append([]ServicePort(nil), payload.ServicePorts...)
	}
	if len(ports) > 0 {
		name := computeServiceName(in, spec)
		if err := d.backend.UpsertService(Service{
			Namespace:   namespace(in),
			Name:        name,
			Hash:        hash,
			Type:        firstNonEmpty(payload.ServiceType, "ClusterIP"),
			Labels:      driverutil.Labels("kubernetes", in, hash),
			Annotations: payload.ServiceAnnotations,
			Selector:    map[string]string{"guardian.asset": in.Asset.Name},
			Ports:       ports,
		}); err != nil {
			return registry.AssetResult{}, err
		}
		if port := firstServicePort(ports); port > 0 {
			outputs["address"] = fmt.Sprintf("%s.%s.svc.cluster.local:%d", name, namespace(in), port)
		}
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ComputeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return err
	}
	if spec.ObserveExisting {
		return nil
	}
	if err := d.backend.DeleteDeployment(namespace(in), computeDeploymentName(in, spec)); err != nil {
		return err
	}
	return d.backend.DeleteService(namespace(in), computeServiceName(in, spec))
}

func (d *TraefikRouteDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *TraefikRouteDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "Traefik route is managed by bootstrap"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "Traefik route metadata is ready"}, nil
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
	return d.checkReferences(ctx, in)
}

func (d *LoadBalancerDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	return d.observeServiceBackedState(ctx, in, workloadName(in, "lb"), serviceName(in, "lb"), "kubernetes load balancer is ready")
}

func (d *LoadBalancerDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	serviceType := resolvedKubernetesServiceType(spec.ServiceType, payload.ServiceType, "LoadBalancer")
	hash := hashWithPayload(loadBalancerHash(in, spec), payload)
	deployment, ok, err := d.backend.GetDeployment(namespace(in), workloadName(in, "lb"))
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || deployment.Hash != hash {
		return changedDrift(in.Asset.Name, "kubernetes load balancer deployment differs"), nil
	}
	service, ok, err := d.backend.GetService(namespace(in), serviceName(in, "lb"))
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || service.Hash != hash || service.Type != serviceType {
		return changedDrift(in.Asset.Name, "kubernetes load balancer service differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "kubernetes load balancer is in sync"), nil
}

func (d *LoadBalancerDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeLoadBalancer(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	serviceType := resolvedKubernetesServiceType(spec.ServiceType, payload.ServiceType, "LoadBalancer")
	hash := hashWithPayload(loadBalancerHash(in, spec), payload)
	bootstrapConfig, err := d.loadBalancerBootstrap(in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	container := Container{
		Name:            "lb-edge",
		Image:           loadBalancerContainerImage(),
		ImagePullPolicy: "IfNotPresent",
		Env: map[string]string{
			"LB_BOOTSTRAP": bootstrapConfig,
		},
		Ports: toListenerServicePorts(spec.Listeners),
	}
	deployment := Deployment{
		Namespace:         namespace(in),
		Name:              workloadName(in, "lb"),
		Kind:              "LoadBalancer",
		Hash:              hash,
		Labels:            driverutil.Labels("kubernetes", in, hash),
		Replicas:          1,
		ReadyReplicas:     1,
		AvailableReplicas: 1,
		Container:         container,
	}
	applyWorkloadPayload(&deployment, payload)
	markDeploymentReady(&deployment)
	if err := d.backend.UpsertDeployment(deployment); err != nil {
		return registry.AssetResult{}, err
	}
	ports := toListenerServicePorts(spec.Listeners)
	if len(payload.ServicePorts) > 0 {
		ports = append([]ServicePort(nil), payload.ServicePorts...)
	}
	if err := d.backend.UpsertService(Service{
		Namespace:   namespace(in),
		Name:        serviceName(in, "lb"),
		Hash:        hash,
		Type:        serviceType,
		Labels:      driverutil.Labels("kubernetes", in, hash),
		Annotations: payload.ServiceAnnotations,
		Selector:    map[string]string{"guardian.asset": in.Asset.Name},
		Ports:       ports,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	outputs := map[string]string{"id": workloadName(in, "lb")}
	if port := firstServicePort(ports); port > 0 {
		outputs["address"] = fmt.Sprintf("%s.%s.svc.cluster.local:%d", serviceName(in, "lb"), namespace(in), port)
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *LoadBalancerDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.backend.DeleteDeployment(namespace(in), workloadName(in, "lb")); err != nil {
		return err
	}
	return d.backend.DeleteService(namespace(in), serviceName(in, "lb"))
}

func (d *ObjectStoreDriver) Check(ctx context.Context, in registry.AssetInput) error {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return err
	}
	if spec.Endpoint != "" {
		return ctx.Err()
	}
	return d.checkReferences(ctx, in)
}

func (d *ObjectStoreDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return nil, nil, err
	}
	if spec.Endpoint != "" {
		return observeExternalObjectStore(ctx, spec.Endpoint)
	}
	return d.observeServiceBackedState(ctx, in, workloadName(in, "obj"), serviceName(in, "obj"), "kubernetes object store is ready")
}

func (d *ObjectStoreDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if spec.Endpoint != "" {
		return externalObjectStoreDiff(ctx, in, spec.Endpoint)
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffServiceBackedDeployment(ctx, in, workloadName(in, "obj"), serviceName(in, "obj"), hashWithPayload(objectStoreHash(in), payload))
}

func (d *ObjectStoreDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeObjectStore(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	if spec.Endpoint != "" {
		return registry.AssetResult{Outputs: map[string]string{
			"id":       in.Asset.Name,
			"endpoint": spec.Endpoint,
		}}, nil
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(objectStoreHash(in), payload)
	container := Container{
		Name:         "minio",
		Image:        "minio/minio:latest",
		Command:      []string{"minio"},
		Args:         []string{"server", "/data", "--console-address=:9001"},
		Env:          map[string]string{"MINIO_ROOT_USER": "minio", "MINIO_ROOT_PASSWORD": "minio123"},
		Ports:        []ServicePort{{Name: "api", Port: 9000, TargetPort: 9000, Protocol: "TCP"}, {Name: "console", Port: 9001, TargetPort: 9001, Protocol: "TCP"}},
		VolumeMounts: objectStoreMounts(in, spec),
	}
	deployment := Deployment{
		Namespace:         namespace(in),
		Name:              workloadName(in, "obj"),
		Kind:              "ObjectStore",
		Hash:              hash,
		Labels:            driverutil.Labels("kubernetes", in, hash),
		Replicas:          1,
		ReadyReplicas:     1,
		AvailableReplicas: 1,
		Container:         container,
	}
	applyWorkloadPayload(&deployment, payload)
	markDeploymentReady(&deployment)
	if err := d.backend.UpsertDeployment(deployment); err != nil {
		return registry.AssetResult{}, err
	}
	servicePorts := container.Ports
	if len(payload.ServicePorts) > 0 {
		servicePorts = append([]ServicePort(nil), payload.ServicePorts...)
	}
	if err := d.backend.UpsertService(Service{
		Namespace: namespace(in),
		Name:      serviceName(in, "obj"),
		Hash:      hash,
		Type:      firstNonEmpty(payload.ServiceType, "ClusterIP"),
		Labels:    driverutil.Labels("kubernetes", in, hash),
		Selector:  map[string]string{"guardian.asset": in.Asset.Name},
		Ports:     servicePorts,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	port := firstServicePort(servicePorts)
	return registry.AssetResult{Outputs: map[string]string{
		"id":       workloadName(in, "obj"),
		"endpoint": fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName(in, "obj"), namespace(in), port),
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
	if err := d.backend.DeleteDeployment(namespace(in), workloadName(in, "obj")); err != nil {
		return err
	}
	return d.backend.DeleteService(namespace(in), serviceName(in, "obj"))
}

func (d *SQLDatabaseDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return d.checkReferences(ctx, in)
}

func (d *SQLDatabaseDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	return d.observeServiceBackedState(ctx, in, workloadName(in, "db"), serviceName(in, "db"), "kubernetes database is ready")
}

func (d *SQLDatabaseDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffServiceBackedDeployment(ctx, in, workloadName(in, "db"), serviceName(in, "db"), hashWithPayload(sqlDatabaseHash(in), payload))
}

func (d *SQLDatabaseDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	image, port, env := sqlDatabaseImagePortEnv(spec)
	hash := hashWithPayload(sqlDatabaseHash(in), payload)
	container := Container{
		Name:         "database",
		Image:        image,
		Env:          env,
		Ports:        []ServicePort{{Name: "db", Protocol: "TCP", Port: port, TargetPort: port}},
		VolumeMounts: sqlDatabaseMounts(in, spec),
	}
	deployment := Deployment{
		Namespace:         namespace(in),
		Name:              workloadName(in, "db"),
		Kind:              "SQLDatabase",
		Hash:              hash,
		Labels:            driverutil.Labels("kubernetes", in, hash),
		Replicas:          1,
		ReadyReplicas:     1,
		AvailableReplicas: 1,
		Container:         container,
	}
	applyWorkloadPayload(&deployment, payload)
	markDeploymentReady(&deployment)
	if err := d.backend.UpsertDeployment(deployment); err != nil {
		return registry.AssetResult{}, err
	}
	servicePorts := container.Ports
	if len(payload.ServicePorts) > 0 {
		servicePorts = append([]ServicePort(nil), payload.ServicePorts...)
	}
	if err := d.backend.UpsertService(Service{
		Namespace: namespace(in),
		Name:      serviceName(in, "db"),
		Hash:      hash,
		Type:      firstNonEmpty(payload.ServiceType, "ClusterIP"),
		Labels:    driverutil.Labels("kubernetes", in, hash),
		Selector:  map[string]string{"guardian.asset": in.Asset.Name},
		Ports:     servicePorts,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	engine := strings.ToLower(spec.Engine)
	resolvedPort := firstServicePort(servicePorts)
	return registry.AssetResult{Outputs: map[string]string{
		"id":     workloadName(in, "db"),
		"engine": engine,
		"url":    fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d/%s", engine, serviceName(in, "db"), namespace(in), resolvedPort, defaultDatabaseName(spec, in.Asset.Name)),
	}}, nil
}

func (d *SQLDatabaseDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.backend.DeleteDeployment(namespace(in), workloadName(in, "db")); err != nil {
		return err
	}
	return d.backend.DeleteService(namespace(in), serviceName(in, "db"))
}

func (d *ObservabilityDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return d.checkReferences(ctx, in)
}

func (d *ObservabilityDriver) ObserveState(ctx context.Context, in registry.AssetInput) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	return d.observeServiceBackedState(ctx, in, workloadName(in, "obs"), serviceName(in, "obs"), "kubernetes observability stack is ready")
}

func (d *ObservabilityDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	return d.diffServiceBackedDeployment(ctx, in, workloadName(in, "obs"), serviceName(in, "obs"), hashWithPayload(observabilityHash(in), payload))
}

func (d *ObservabilityDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	spec, err := decodeObservability(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	payload, err := loadWorkloadPayload(ctx, in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := hashWithPayload(observabilityHash(in), payload)
	port := observabilityPort(spec)
	container := Container{
		Name:         "otelcol",
		Image:        "otel/opentelemetry-collector-contrib:0.102.1",
		Command:      []string{"otelcol-contrib"},
		Args:         []string{"--config=/etc/otelcol/config.yaml"},
		Ports:        []ServicePort{{Name: "otlp", Protocol: "TCP", Port: port, TargetPort: port}},
		VolumeMounts: observabilityMounts(in, spec),
		InlineFiles:  observabilityInlineFiles(spec),
	}
	if spec.Config != "" {
		container.VolumeMounts = append(container.VolumeMounts, VolumeMount{
			SourceKind: "ConfigMap",
			SourceName: configMapName(in, spec.Config),
			MountPath:  "/etc/otelcol/config.yaml",
			SubPath:    firstConfigPath(in, spec.Config),
			ReadOnly:   true,
		})
	} else if len(container.InlineFiles) > 0 {
		container.VolumeMounts = append(container.VolumeMounts, VolumeMount{
			SourceKind: "InlineFile",
			SourceName: "config.yaml",
			MountPath:  "/etc/otelcol/config.yaml",
			SubPath:    "config.yaml",
			ReadOnly:   true,
		})
	}
	deployment := Deployment{
		Namespace:         namespace(in),
		Name:              workloadName(in, "obs"),
		Kind:              "Observability",
		Hash:              hash,
		Labels:            driverutil.Labels("kubernetes", in, hash),
		Replicas:          1,
		ReadyReplicas:     1,
		AvailableReplicas: 1,
		Container:         container,
	}
	applyWorkloadPayload(&deployment, payload)
	markDeploymentReady(&deployment)
	if err := d.backend.UpsertDeployment(deployment); err != nil {
		return registry.AssetResult{}, err
	}
	servicePorts := container.Ports
	if len(payload.ServicePorts) > 0 {
		servicePorts = append([]ServicePort(nil), payload.ServicePorts...)
	}
	if err := d.backend.UpsertService(Service{
		Namespace: namespace(in),
		Name:      serviceName(in, "obs"),
		Hash:      hash,
		Type:      firstNonEmpty(payload.ServiceType, "ClusterIP"),
		Labels:    driverutil.Labels("kubernetes", in, hash),
		Selector:  map[string]string{"guardian.asset": in.Asset.Name},
		Ports:     servicePorts,
	}); err != nil {
		return registry.AssetResult{}, err
	}
	resolvedPort := firstServicePort(servicePorts)
	return registry.AssetResult{Outputs: map[string]string{
		"id":       workloadName(in, "obs"),
		"endpoint": fmt.Sprintf("%s.%s.svc.cluster.local:%d", serviceName(in, "obs"), namespace(in), resolvedPort),
	}}, nil
}

func (d *ObservabilityDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.backend.DeleteDeployment(namespace(in), workloadName(in, "obs")); err != nil {
		return err
	}
	return d.backend.DeleteService(namespace(in), serviceName(in, "obs"))
}

func (d *baseDriver) checkReferences(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, dep := range in.Asset.DependsOn {
		asset, ok := in.Assets[dep]
		if !ok {
			continue
		}
		switch asset.Type {
		case "Config":
			if _, _, err := d.backend.GetConfigMap(namespace(in), configMapName(in, dep)); err != nil {
				return err
			}
		case "Volume":
			_, typed, err := driverutil.DecodeNamedAsset(in, dep)
			if err != nil {
				return err
			}
			spec := typed.(*assetdefs.VolumeSpec)
			if driverutil.BoolValue(spec.Ephemeral) {
				continue
			}
			if _, _, err := d.backend.GetClaim(namespace(in), claimName(in, dep)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *baseDriver) observeApplyReadiness(ctx context.Context, in registry.AssetInput) (*taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.checkReferences(ctx, in); err != nil {
		return &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessBlocked, Summary: err.Error()}, nil
	}
	return &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "dependencies resolved"}, nil
}

func (d *baseDriver) observeComputeHealth(ctx context.Context, in registry.AssetInput, expectService bool) (*taskdomain.HealthObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deploymentName := workloadName(in, "compute")
	serviceLookupName := serviceName(in, "compute")
	if strings.EqualFold(strings.TrimSpace(in.Asset.Type), assetdomain.TypeCompute) {
		spec, err := decodeCompute(in)
		if err != nil {
			return nil, err
		}
		deploymentName = computeDeploymentName(in, spec)
		serviceLookupName = computeServiceName(in, spec)
	}
	deployment, ok, err := d.backend.GetDeployment(namespace(in), deploymentName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes deployment %s missing", deploymentName)}, nil
	}
	if deployment.CrashLoopBackOff {
		reason := deployment.PodFailureReason
		if reason == "" {
			reason = "CrashLoopBackOff"
		}
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes deployment has pods in %s", reason)}, nil
	}
	if deployment.ReadyReplicas < deployment.Replicas || deployment.AvailableReplicas < deployment.Replicas {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "kubernetes deployment is not ready"}, nil
	}
	if !expectService {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "kubernetes workload is ready"}, nil
	}
	service, ok, err := d.backend.GetService(namespace(in), serviceLookupName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes service %s missing", serviceLookupName)}, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: fmt.Sprintf("kubernetes service %s is ready", service.Name)}, nil
}

func (d *baseDriver) observeServiceBackedState(ctx context.Context, in registry.AssetInput, deploymentName, serviceName, healthySummary string) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	readiness, err := d.observeApplyReadiness(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	health, err := d.observeServiceBackedHealth(ctx, in, deploymentName, serviceName, healthySummary)
	if err != nil {
		return nil, nil, err
	}
	return health, readiness, nil
}

func (d *baseDriver) observeServiceBackedHealth(ctx context.Context, in registry.AssetInput, deploymentName, svcName, healthySummary string) (*taskdomain.HealthObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deployment, ok, err := d.backend.GetDeployment(namespace(in), deploymentName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes deployment %s missing", deploymentName)}, nil
	}
	if deployment.CrashLoopBackOff {
		reason := deployment.PodFailureReason
		if reason == "" {
			reason = "CrashLoopBackOff"
		}
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes deployment has pods in %s", reason)}, nil
	}
	if deployment.ReadyReplicas < deployment.Replicas || deployment.AvailableReplicas < deployment.Replicas {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthDegraded, Summary: "kubernetes deployment is not ready"}, nil
	}
	service, ok, err := d.backend.GetService(namespace(in), svcName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &taskdomain.HealthObservation{Status: taskdomain.HealthUnhealthy, Summary: fmt.Sprintf("kubernetes service %s missing", svcName)}, nil
	}
	if healthySummary == "" {
		healthySummary = fmt.Sprintf("kubernetes service %s is ready", service.Name)
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: healthySummary}, nil
}

func (d *baseDriver) diffServiceBackedDeployment(ctx context.Context, in registry.AssetInput, deploymentName, svcName, hash string) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	deployment, ok, err := d.backend.GetDeployment(namespace(in), deploymentName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || deployment.Hash != hash {
		return changedDrift(in.Asset.Name, "kubernetes deployment differs"), nil
	}
	service, ok, err := d.backend.GetService(namespace(in), svcName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || service.Hash != hash {
		return changedDrift(in.Asset.Name, "kubernetes service differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "kubernetes resource is in sync"), nil
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

func objectStoreHash(in registry.AssetInput) string { return driverutil.CompositeHash(in) }

func observeExternalObjectStore(ctx context.Context, endpoint string) (*taskdomain.HealthObservation, *taskdomain.ApplyReadiness, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(endpoint) == "" {
		return nil, nil, nil
	}
	return &taskdomain.HealthObservation{Status: taskdomain.HealthHealthy, Summary: "external object store reference resolved"}, &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "external object store reference resolved"}, nil
}

func externalObjectStoreDiff(ctx context.Context, in registry.AssetInput, endpoint string) (taskdomain.DriftReport, error) {
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
	want := strings.TrimSpace(endpoint)
	got := strings.TrimSpace(state.Outputs[in.Asset.Name+".endpoint"])
	if got != want {
		return changedDrift(in.Asset.Name, "external object store endpoint differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "external object store reference"), nil
}

func sqlDatabaseHash(in registry.AssetInput) string   { return driverutil.CompositeHash(in) }
func observabilityHash(in registry.AssetInput) string { return driverutil.CompositeHash(in) }

func buildComputeContainer(ctx context.Context, in registry.AssetInput, resolver secrets.Resolver, spec *assetdefs.ComputeSpec) (Container, error) {
	env, err := driverutil.ResolveEnv(ctx, resolver, spec.Env)
	if err != nil {
		return Container{}, err
	}
	container := Container{
		Name:            in.Asset.Name,
		Image:           spec.Image,
		ImagePullPolicy: spec.ImagePullPolicy,
		Command:         []string(spec.Command),
		Args:            []string(spec.Args),
		Env:             env,
		Ports:           toServicePorts(spec.Ports),
		Privileged:      driverutil.BoolValue(spec.Privileged),
		Capabilities:    append([]string(nil), spec.Capabilities...),
	}
	if r := spec.Resources; r != nil {
		container.Resources = ContainerResources{
			CPURequest:    r.Requests.CPU,
			CPULimit:      r.Limits.CPU,
			MemoryRequest: r.Requests.Memory,
			MemoryLimit:   r.Limits.Memory,
		}
	}
	for _, mount := range spec.VolumeMounts {
		_, typed, err := driverutil.DecodeNamedAsset(in, mount.Volume)
		if err != nil {
			return Container{}, err
		}
		volume := typed.(*assetdefs.VolumeSpec)
		vm := VolumeMount{MountPath: mount.Path, ReadOnly: mount.ReadOnly}
		if driverutil.BoolValue(volume.Ephemeral) {
			vm.SourceKind = "EmptyDir"
			vm.SourceName = mount.Volume
			vm.Ephemeral = true
		} else {
			vm.SourceKind = "PersistentVolumeClaim"
			vm.SourceName = claimName(in, mount.Volume)
		}
		container.VolumeMounts = append(container.VolumeMounts, vm)
	}
	for _, mount := range spec.ConfigMounts {
		cm := VolumeMount{
			SourceKind: "ConfigMap",
			SourceName: configMapName(in, mount.Config),
			MountPath:  mount.Path,
			ReadOnly:   mount.ReadOnly,
		}
		if fileName := firstConfigPath(in, mount.Config); fileName != "" && configMountUsesSubPath(mount.Path, fileName) {
			cm.SubPath = fileName
		}
		container.VolumeMounts = append(container.VolumeMounts, cm)
	}
	for _, mount := range spec.HostBindMounts {
		container.VolumeMounts = append(container.VolumeMounts, VolumeMount{
			SourceKind: "HostPath",
			SourceName: mount.Source,
			MountPath:  mount.Target,
			ReadOnly:   mount.ReadOnly,
		})
	}
	return container, nil
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
			port := matchComputePort(compute, *listener.Port)
			service := driverutil.ResourceName("k8s-svc-compute", in.Target, in.PartitionName, in.IntentName, target)
			backends = append(backends, fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, namespace(in), port))
		}
		if len(backends) == 0 {
			continue
		}
		entries = append(entries, driverutil.BuildBootstrapEntry(listener, backends))
	}
	return strings.Join(entries, ";"), nil
}

func loadBalancerContainerImage() string {
	if override := strings.TrimSpace(os.Getenv("GUARDIAN_LB_IMAGE")); override != "" {
		return override
	}
	return "lb:latest"
}

func generateHAProxyConfig(in registry.AssetInput, spec *assetdefs.LoadBalancerSpec) (string, error) {
	var b strings.Builder
	b.WriteString("global\n  daemon\n")
	b.WriteString("defaults\n  mode tcp\n  timeout connect 5s\n  timeout client 30s\n  timeout server 30s\n")
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
			port := matchComputePort(compute, *listener.Port)
			service := driverutil.ResourceName("k8s-svc-compute", in.Target, in.PartitionName, in.IntentName, target)
			b.WriteString(fmt.Sprintf("  server %s %s.%s.svc.cluster.local:%d check\n", target, service, namespace(in), port))
		}
	}
	return b.String(), nil
}

func matchComputePort(spec *assetdefs.ComputeSpec, prefer int) int {
	for _, port := range spec.Ports {
		if port.ServicePort != nil && (*port.ServicePort == prefer || prefer == 0) {
			return *port.ServicePort
		}
		if port.Port != nil && (*port.Port == prefer || prefer == 0) {
			return *port.Port
		}
		if port.ContainerPort != nil && (*port.ContainerPort == prefer || prefer == 0) {
			return *port.ContainerPort
		}
	}
	return driverutil.FirstPort(spec.Ports)
}

func toServicePorts(ports []assetdefs.PortSpec) []ServicePort {
	out := make([]ServicePort, 0, len(ports))
	for _, port := range ports {
		containerPort := 0
		if port.ContainerPort != nil {
			containerPort = *port.ContainerPort
		} else if port.Port != nil {
			containerPort = *port.Port
		}
		servicePort := containerPort
		if port.ServicePort != nil {
			servicePort = *port.ServicePort
		} else if port.Port != nil {
			servicePort = *port.Port
		}
		hostPort := 0
		if strings.TrimSpace(port.DynamicHostname) == "" {
			hostPort = intPtrVal(port.HostPort)
		}
		out = append(out, ServicePort{
			Name:       port.Name,
			Protocol:   firstNonEmpty(port.Protocol, "TCP"),
			Port:       servicePort,
			TargetPort: containerPort,
			HostPort:   hostPort,
		})
	}
	return out
}

func toListenerServicePorts(listeners []assetdefs.ListenerSpec) []ServicePort {
	out := make([]ServicePort, 0, len(listeners))
	for _, listener := range listeners {
		if listener.Port == nil {
			continue
		}
		out = append(out, ServicePort{
			Name:       listener.Name,
			Protocol:   firstNonEmpty(listener.Protocol, "TCP"),
			Port:       *listener.Port,
			TargetPort: *listener.Port,
		})
	}
	return out
}

func firstServicePort(ports []ServicePort) int {
	for _, port := range ports {
		if port.Port > 0 {
			return port.Port
		}
		if port.TargetPort > 0 {
			return port.TargetPort
		}
	}
	return 0
}

func objectStoreMounts(in registry.AssetInput, spec *assetdefs.ObjectStoreSpec) []VolumeMount {
	out := make([]VolumeMount, 0)
	if spec.Volume != "" {
		out = append(out, volumeMountFromRef(in, spec.Volume, "/data"))
	}
	if spec.Config != "" {
		out = append(out, VolumeMount{SourceKind: "ConfigMap", SourceName: configMapName(in, spec.Config), MountPath: "/etc/minio", ReadOnly: true})
	}
	return out
}

func sqlDatabaseMounts(in registry.AssetInput, spec *assetdefs.SQLDatabaseSpec) []VolumeMount {
	out := make([]VolumeMount, 0)
	if spec.Volume != "" {
		out = append(out, volumeMountFromRef(in, spec.Volume, databaseMountPath(spec.Engine)))
	}
	if spec.Config != "" {
		out = append(out, VolumeMount{SourceKind: "ConfigMap", SourceName: configMapName(in, spec.Config), MountPath: "/etc/guardian-db", ReadOnly: true})
	}
	return out
}

func observabilityMounts(in registry.AssetInput, spec *assetdefs.ObservabilitySpec) []VolumeMount {
	out := make([]VolumeMount, 0)
	if spec.Volume != "" {
		out = append(out, volumeMountFromRef(in, spec.Volume, "/var/lib/otelcol"))
	}
	return out
}

func volumeMountFromRef(in registry.AssetInput, assetName, path string) VolumeMount {
	_, typed, err := driverutil.DecodeNamedAsset(in, assetName)
	if err != nil {
		return VolumeMount{SourceKind: "PersistentVolumeClaim", SourceName: claimName(in, assetName), MountPath: path}
	}
	volume := typed.(*assetdefs.VolumeSpec)
	if driverutil.BoolValue(volume.Ephemeral) {
		return VolumeMount{SourceKind: "EmptyDir", SourceName: assetName, MountPath: path, Ephemeral: true}
	}
	return VolumeMount{SourceKind: "PersistentVolumeClaim", SourceName: claimName(in, assetName), MountPath: path}
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

func observabilityInlineFiles(spec *assetdefs.ObservabilitySpec) map[string]string {
	if spec.Config != "" {
		return nil
	}
	endpoint := spec.Endpoint
	if endpoint == "" {
		endpoint = "0.0.0.0:4317"
	}
	exporter := "logging"
	if len(spec.Exporters) > 0 {
		exporter = spec.Exporters[0]
	}
	return map[string]string{
		"config.yaml": fmt.Sprintf("receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: %s\nexporters:\n  %s: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [%s]\n", endpoint, exporter, exporter),
	}
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

func configMountUsesSubPath(mountPath, fileName string) bool {
	cleanPath := strings.TrimSuffix(strings.TrimSpace(mountPath), "/")
	if cleanPath == "" || fileName == "" {
		return false
	}
	return path.Base(cleanPath) == fileName
}

func workloadName(in registry.AssetInput, suffix string) string {
	return driverutil.ResourceName("k8s-"+suffix, in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func computeDeploymentName(in registry.AssetInput, spec *assetdefs.ComputeSpec) string {
	if spec != nil && spec.ObserveExisting {
		if explicit := strings.TrimSpace(spec.ExistingDeploymentName); explicit != "" {
			return explicit
		}
		return in.Asset.Name
	}
	return workloadName(in, "compute")
}

func serviceName(in registry.AssetInput, suffix string) string {
	return driverutil.ResourceName("k8s-svc-"+suffix, in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func computeServiceName(in registry.AssetInput, spec *assetdefs.ComputeSpec) string {
	if spec != nil && spec.ObserveExisting {
		if explicit := strings.TrimSpace(spec.ExistingServiceName); explicit != "" {
			return explicit
		}
		return in.Asset.Name
	}
	return serviceName(in, "compute")
}

func claimName(in registry.AssetInput, assetName string) string {
	return driverutil.ResourceName("k8s-pvc", in.Target, in.PartitionName, in.IntentName, assetName)
}

func configMapName(in registry.AssetInput, assetName string) string {
	return driverutil.ResourceName("k8s-cm", in.Target, in.PartitionName, in.IntentName, assetName)
}

func namespace(in registry.AssetInput) string {
	if strings.TrimSpace(in.Target.Namespace) == "" {
		return "default"
	}
	return in.Target.Namespace
}

func defaultDatabaseName(spec *assetdefs.SQLDatabaseSpec, fallback string) string {
	if strings.TrimSpace(spec.Database) != "" {
		return spec.Database
	}
	return fallback
}

func databaseMountPath(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql", "mariadb":
		return "/var/lib/mysql"
	default:
		return "/var/lib/postgresql/data"
	}
}

func defaultPort(port *int, fallback int) int {
	if port != nil && *port > 0 {
		return *port
	}
	return fallback
}

func resolvedKubernetesServiceType(specValue, payloadValue, fallback string) string {
	return firstNonEmpty(payloadValue, firstNonEmpty(specValue, fallback))
}

func markDeploymentReady(deployment *Deployment) {
	if deployment == nil {
		return
	}
	if deployment.Replicas < 1 {
		deployment.Replicas = 1
	}
	deployment.ReadyReplicas = deployment.Replicas
	deployment.AvailableReplicas = deployment.Replicas
}

func changedDrift(asset, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{Status: "Changed", Summary: summary, ChangedAssets: []string{asset}}
}

func inSyncDrift(asset, summary string) taskdomain.DriftReport {
	return taskdomain.DriftReport{Status: "InSync", Summary: summary}
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func intPtrVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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
