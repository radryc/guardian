package kubernetesdriver

import "context"

type BackendAPI interface {
	UpsertConfigMap(ctx context.Context, cm ConfigMap) error
	GetConfigMap(ctx context.Context, namespace, name string) (ConfigMap, bool, error)
	DeleteConfigMap(ctx context.Context, namespace, name string) error

	UpsertClaim(ctx context.Context, claim PersistentVolumeClaim) error
	GetClaim(ctx context.Context, namespace, name string) (PersistentVolumeClaim, bool, error)
	DeleteClaim(ctx context.Context, namespace, name string) error

	UpsertDeployment(ctx context.Context, deployment Deployment) error
	GetDeployment(ctx context.Context, namespace, name string) (Deployment, bool, error)
	DeleteDeployment(ctx context.Context, namespace, name string) error

	GetPodEvents(ctx context.Context, namespace, podName string) ([]string, error)

	UpsertService(ctx context.Context, service Service) error
	GetService(ctx context.Context, namespace, name string) (Service, bool, error)
	DeleteService(ctx context.Context, namespace, name string) error

	UpsertNetworkPolicy(ctx context.Context, policy NetworkPolicy) error
	GetNetworkPolicy(ctx context.Context, namespace, name string) (NetworkPolicy, bool, error)
	DeleteNetworkPolicy(ctx context.Context, namespace, name string) error

	ApplyManifest(ctx context.Context, namespace string, manifest []byte) error
}
