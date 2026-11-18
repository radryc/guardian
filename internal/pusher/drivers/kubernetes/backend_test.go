package kubernetesdriver

import (
	"context"
	"sync"
)

type Backend struct {
	mu              sync.Mutex
	configMaps      map[string]ConfigMap
	claims          map[string]PersistentVolumeClaim
	deployments     map[string]Deployment
	services        map[string]Service
	networkPolicies map[string]NetworkPolicy
}

func NewBackend() *Backend {
	return &Backend{
		configMaps:      map[string]ConfigMap{},
		claims:          map[string]PersistentVolumeClaim{},
		deployments:     map[string]Deployment{},
		services:        map[string]Service{},
		networkPolicies: map[string]NetworkPolicy{},
	}
}

func (b *Backend) UpsertConfigMap(ctx context.Context, cm ConfigMap) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configMaps[key(cm.Namespace, cm.Name)] = cloneConfigMap(cm)
	return nil
}

func (b *Backend) GetConfigMap(ctx context.Context, namespace, name string) (ConfigMap, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cm, ok := b.configMaps[key(namespace, name)]
	return cloneConfigMap(cm), ok, nil
}

func (b *Backend) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.configMaps, key(namespace, name))
	return nil
}

func (b *Backend) UpsertClaim(ctx context.Context, claim PersistentVolumeClaim) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claims[key(claim.Namespace, claim.Name)] = cloneClaim(claim)
	return nil
}

func (b *Backend) GetClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaim, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	claim, ok := b.claims[key(namespace, name)]
	return cloneClaim(claim), ok, nil
}

func (b *Backend) DeleteClaim(ctx context.Context, namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.claims, key(namespace, name))
	return nil
}

func (b *Backend) UpsertDeployment(ctx context.Context, deployment Deployment) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deployments[key(deployment.Namespace, deployment.Name)] = cloneDeployment(deployment)
	return nil
}

func (b *Backend) GetDeployment(ctx context.Context, namespace, name string) (Deployment, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	deployment, ok := b.deployments[key(namespace, name)]
	return cloneDeployment(deployment), ok, nil
}

func (b *Backend) DeleteDeployment(ctx context.Context, namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.deployments, key(namespace, name))
	return nil
}

func (b *Backend) UpsertService(ctx context.Context, service Service) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.services[key(service.Namespace, service.Name)] = cloneService(service)
	return nil
}

func (b *Backend) GetService(ctx context.Context, namespace, name string) (Service, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	service, ok := b.services[key(namespace, name)]
	return cloneService(service), ok, nil
}

func (b *Backend) DeleteService(ctx context.Context, namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.services, key(namespace, name))
	return nil
}

func (b *Backend) UpsertNetworkPolicy(ctx context.Context, policy NetworkPolicy) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.networkPolicies[key(policy.Namespace, policy.Name)] = cloneNetworkPolicy(policy)
	return nil
}

func (b *Backend) GetNetworkPolicy(ctx context.Context, namespace, name string) (NetworkPolicy, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	policy, ok := b.networkPolicies[key(namespace, name)]
	return cloneNetworkPolicy(policy), ok, nil
}

func (b *Backend) DeleteNetworkPolicy(ctx context.Context, namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.networkPolicies, key(namespace, name))
	return nil
}

func (b *Backend) GetPodEvents(ctx context.Context, namespace, podName string) ([]string, error) {
	return nil, nil
}

func (b *Backend) ApplyManifest(ctx context.Context, namespace string, manifest []byte) error {
	return nil
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

func cloneConfigMap(in ConfigMap) ConfigMap {
	return ConfigMap{
		Namespace:  in.Namespace,
		Name:       in.Name,
		Hash:       in.Hash,
		Labels:     cloneStringMap(in.Labels),
		Data:       cloneStringMap(in.Data),
		BinaryData: cloneStringMap(in.BinaryData),
	}
}

func cloneClaim(in PersistentVolumeClaim) PersistentVolumeClaim {
	return PersistentVolumeClaim{
		Namespace:    in.Namespace,
		Name:         in.Name,
		Hash:         in.Hash,
		Labels:       cloneStringMap(in.Labels),
		Size:         in.Size,
		AccessMode:   in.AccessMode,
		StorageClass: in.StorageClass,
		Ephemeral:    in.Ephemeral,
	}
}

func cloneDeployment(in Deployment) Deployment {
	return Deployment{
		Namespace:          in.Namespace,
		Name:               in.Name,
		Kind:               in.Kind,
		Hash:               in.Hash,
		Labels:             cloneStringMap(in.Labels),
		Replicas:           in.Replicas,
		ReadyReplicas:      in.ReadyReplicas,
		AvailableReplicas:  in.AvailableReplicas,
		Container:          cloneContainer(in.Container),
		CrashLoopBackOff:   in.CrashLoopBackOff,
		PodFailureReason:   in.PodFailureReason,
		PodFailureMessage:  in.PodFailureMessage,
		PodFailurePodName:  in.PodFailurePodName,
		ServiceAccountName: in.ServiceAccountName,
		HostUsers:          in.HostUsers,
	}
}

func cloneContainer(in Container) Container {
	return Container{
		Name:            in.Name,
		Image:           in.Image,
		ImagePullPolicy: in.ImagePullPolicy,
		Command:         append([]string(nil), in.Command...),
		Args:            append([]string(nil), in.Args...),
		Env:             cloneStringMap(in.Env),
		Ports:           append([]ServicePort(nil), in.Ports...),
		VolumeMounts:    append([]VolumeMount(nil), in.VolumeMounts...),
		InlineFiles:     cloneStringMap(in.InlineFiles),
		ReadinessProbe:  cloneProbe(in.ReadinessProbe),
		LivenessProbe:   cloneProbe(in.LivenessProbe),
		StartupProbe:    cloneProbe(in.StartupProbe),
		Privileged:      in.Privileged,
		Capabilities:    append([]string(nil), in.Capabilities...),
		Resources:       cloneContainerResources(in.Resources),
	}
}

func cloneContainerResources(in ContainerResources) ContainerResources {
	out := ContainerResources{
		CPURequest:    in.CPURequest,
		CPULimit:      in.CPULimit,
		MemoryRequest: in.MemoryRequest,
		MemoryLimit:   in.MemoryLimit,
	}
	if len(in.ExtendedResources) > 0 {
		out.ExtendedResources = make(map[string]string, len(in.ExtendedResources))
		for k, v := range in.ExtendedResources {
			out.ExtendedResources[k] = v
		}
	}
	return out
}

func cloneService(in Service) Service {
	return Service{
		Namespace:   in.Namespace,
		Name:        in.Name,
		Hash:        in.Hash,
		Type:        in.Type,
		Labels:      cloneStringMap(in.Labels),
		Annotations: cloneStringMap(in.Annotations),
		Selector:    cloneStringMap(in.Selector),
		Ports:       append([]ServicePort(nil), in.Ports...),
	}
}

func cloneNetworkPolicy(in NetworkPolicy) NetworkPolicy {
	return NetworkPolicy{
		Namespace:    in.Namespace,
		Name:         in.Name,
		Hash:         in.Hash,
		Labels:       cloneStringMap(in.Labels),
		PodSelector:  cloneStringMap(in.PodSelector),
		AllowCluster: in.AllowCluster,
		Internal:     in.Internal,
	}
}
