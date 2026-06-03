package kubernetesdriver

import "sync"

type Backend struct {
	mu          sync.Mutex
	configMaps  map[string]ConfigMap
	claims      map[string]PersistentVolumeClaim
	deployments map[string]Deployment
	services    map[string]Service
}

type ConfigMap struct {
	Namespace string
	Name      string
	Hash      string
	Labels    map[string]string
	Data      map[string]string
}

type PersistentVolumeClaim struct {
	Namespace    string
	Name         string
	Hash         string
	Labels       map[string]string
	Size         string
	AccessMode   string
	StorageClass string
	Ephemeral    bool
}

type ServicePort struct {
	Name       string `yaml:"name,omitempty"`
	Protocol   string `yaml:"protocol,omitempty"`
	Port       int    `yaml:"port,omitempty"`
	TargetPort int    `yaml:"targetPort,omitempty"`
	HostPort   int    `yaml:"hostPort,omitempty"`
}

type VolumeMount struct {
	SourceKind string `yaml:"sourceKind,omitempty"`
	SourceName string `yaml:"sourceName,omitempty"`
	MountPath  string `yaml:"mountPath,omitempty"`
	SubPath    string `yaml:"subPath,omitempty"`
	ReadOnly   bool   `yaml:"readOnly,omitempty"`
	Ephemeral  bool   `yaml:"ephemeral,omitempty"`
}

type ContainerResources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

type ProbeTCPSocket struct {
	Port int `yaml:"port,omitempty" json:"port,omitempty"`
}

type ProbeHTTPGet struct {
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	Port   int    `yaml:"port,omitempty" json:"port,omitempty"`
	Scheme string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
}

type Probe struct {
	TCPSocket           *ProbeTCPSocket `yaml:"tcpSocket,omitempty" json:"tcpSocket,omitempty"`
	HTTPGet             *ProbeHTTPGet   `yaml:"httpGet,omitempty" json:"httpGet,omitempty"`
	InitialDelaySeconds int             `yaml:"initialDelaySeconds,omitempty" json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int             `yaml:"periodSeconds,omitempty" json:"periodSeconds,omitempty"`
	TimeoutSeconds      int             `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds,omitempty"`
	SuccessThreshold    int             `yaml:"successThreshold,omitempty" json:"successThreshold,omitempty"`
	FailureThreshold    int             `yaml:"failureThreshold,omitempty" json:"failureThreshold,omitempty"`
}

type Container struct {
	Name            string
	Image           string
	ImagePullPolicy string
	Command         []string
	Args            []string
	Env             map[string]string
	Ports           []ServicePort
	VolumeMounts    []VolumeMount
	InlineFiles     map[string]string
	ReadinessProbe  *Probe
	Privileged      bool
	Capabilities    []string
	Resources       ContainerResources
}

type Deployment struct {
	Namespace          string
	Name               string
	Kind               string
	Hash               string
	Labels             map[string]string
	Replicas           int
	ReadyReplicas      int
	AvailableReplicas  int
	Container          Container
	CrashLoopBackOff   bool
	PodFailureReason   string // non-empty when CrashLoopBackOff is true; names the exact waiting reason
	ServiceAccountName string
}

type Service struct {
	Namespace   string
	Name        string
	Hash        string
	Type        string
	Labels      map[string]string
	Annotations map[string]string
	Selector    map[string]string
	Ports       []ServicePort
}

func NewBackend() *Backend {
	return &Backend{
		configMaps:  map[string]ConfigMap{},
		claims:      map[string]PersistentVolumeClaim{},
		deployments: map[string]Deployment{},
		services:    map[string]Service{},
	}
}

func (b *Backend) UpsertConfigMap(cm ConfigMap) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configMaps[key(cm.Namespace, cm.Name)] = cloneConfigMap(cm)
	return nil
}

func (b *Backend) GetConfigMap(namespace, name string) (ConfigMap, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cm, ok := b.configMaps[key(namespace, name)]
	return cloneConfigMap(cm), ok, nil
}

func (b *Backend) DeleteConfigMap(namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.configMaps, key(namespace, name))
	return nil
}

func (b *Backend) UpsertClaim(claim PersistentVolumeClaim) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claims[key(claim.Namespace, claim.Name)] = cloneClaim(claim)
	return nil
}

func (b *Backend) GetClaim(namespace, name string) (PersistentVolumeClaim, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	claim, ok := b.claims[key(namespace, name)]
	return cloneClaim(claim), ok, nil
}

func (b *Backend) DeleteClaim(namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.claims, key(namespace, name))
	return nil
}

func (b *Backend) UpsertDeployment(deployment Deployment) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deployments[key(deployment.Namespace, deployment.Name)] = cloneDeployment(deployment)
	return nil
}

func (b *Backend) GetDeployment(namespace, name string) (Deployment, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	deployment, ok := b.deployments[key(namespace, name)]
	return cloneDeployment(deployment), ok, nil
}

func (b *Backend) DeleteDeployment(namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.deployments, key(namespace, name))
	return nil
}

func (b *Backend) UpsertService(service Service) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.services[key(service.Namespace, service.Name)] = cloneService(service)
	return nil
}

func (b *Backend) GetService(namespace, name string) (Service, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	service, ok := b.services[key(namespace, name)]
	return cloneService(service), ok, nil
}

func (b *Backend) DeleteService(namespace, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.services, key(namespace, name))
	return nil
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

func cloneConfigMap(in ConfigMap) ConfigMap {
	return ConfigMap{
		Namespace: in.Namespace,
		Name:      in.Name,
		Hash:      in.Hash,
		Labels:    cloneStringMap(in.Labels),
		Data:      cloneStringMap(in.Data),
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
		ServiceAccountName: in.ServiceAccountName,
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
		Privileged:      in.Privileged,
		Capabilities:    append([]string(nil), in.Capabilities...),
		Resources:       in.Resources,
	}
}

func cloneProbe(in *Probe) *Probe {
	if in == nil {
		return nil
	}
	out := *in
	if in.TCPSocket != nil {
		tcp := *in.TCPSocket
		out.TCPSocket = &tcp
	}
	if in.HTTPGet != nil {
		httpGet := *in.HTTPGet
		out.HTTPGet = &httpGet
	}
	return &out
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

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
