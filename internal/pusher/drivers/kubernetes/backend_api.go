package kubernetesdriver

type BackendAPI interface {
	UpsertConfigMap(cm ConfigMap) error
	GetConfigMap(namespace, name string) (ConfigMap, bool, error)
	DeleteConfigMap(namespace, name string) error

	UpsertClaim(claim PersistentVolumeClaim) error
	GetClaim(namespace, name string) (PersistentVolumeClaim, bool, error)
	DeleteClaim(namespace, name string) error

	UpsertDeployment(deployment Deployment) error
	GetDeployment(namespace, name string) (Deployment, bool, error)
	DeleteDeployment(namespace, name string) error

	UpsertService(service Service) error
	GetService(namespace, name string) (Service, bool, error)
	DeleteService(namespace, name string) error
}
