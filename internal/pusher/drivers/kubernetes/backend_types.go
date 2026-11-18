package kubernetesdriver

type ConfigMap struct {
	Namespace  string
	Name       string
	Hash       string
	Labels     map[string]string
	Data       map[string]string
	BinaryData map[string]string
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
	CPURequest        string
	CPULimit          string
	MemoryRequest     string
	MemoryLimit       string
	ExtendedResources map[string]string
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
	LivenessProbe   *Probe
	StartupProbe    *Probe
	Privileged      bool
	Capabilities    []string
	RunAsUser       *int64
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
	PodFailureMessage  string // non-empty when CrashLoopBackOff is true; contains the detailed waiting message
	PodFailurePodName  string // non-empty when CrashLoopBackOff is true; name of the failing pod
	ServiceAccountName string
	HostUsers          *bool
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

// NetworkPolicy is a managed pod-level connectivity boundary. Pods join the
// policy by carrying the guardian.network.<asset> label; Compute assets label
// themselves for every network they reference.

type NetworkPolicy struct {
	Namespace    string
	Name         string
	Hash         string
	Labels       map[string]string
	PodSelector  map[string]string // matchLabels
	AllowCluster bool              // scope: cluster — also allow ingress from other namespaces
	Internal     bool              // internal: true — deny egress outside the namespace (DNS excepted)
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
