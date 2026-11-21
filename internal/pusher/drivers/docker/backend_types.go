package dockerdriver

type Network struct {
	Name     string
	Hash     string
	Labels   map[string]string
	Driver   string
	Internal bool
}

type Volume struct {
	Name       string
	Hash       string
	Labels     map[string]string
	Size       string
	AccessMode string
	Ephemeral  bool
}

type Config struct {
	Name   string
	Hash   string
	Labels map[string]string
	Files  map[string]string
	Binary bool
}

type PortBinding struct {
	Name          string `yaml:"name,omitempty"`
	Protocol      string `yaml:"protocol,omitempty"`
	ContainerPort int    `yaml:"containerPort,omitempty"`
	HostPort      int    `yaml:"hostPort,omitempty"`
}

type VolumeMount struct {
	Source   string `yaml:"source,omitempty"`
	Target   string `yaml:"target,omitempty"`
	ReadOnly bool   `yaml:"readOnly,omitempty"`
}

type HostBindMount struct {
	Source   string `yaml:"source,omitempty"`
	Target   string `yaml:"target,omitempty"`
	ReadOnly bool   `yaml:"readOnly,omitempty"`
}

type ConfigMount struct {
	Config     string `yaml:"config,omitempty"`
	SourcePath string `yaml:"sourcePath,omitempty"`
	TargetPath string `yaml:"targetPath,omitempty"`
	ReadOnly   bool   `yaml:"readOnly,omitempty"`
}

type ExtraNetwork struct {
	Name    string
	Aliases []string
}

type Container struct {
	Name              string
	Kind              string
	Image             string
	Hash              string
	Labels            map[string]string
	Network           string
	Aliases           []string
	ExtraNetworks     []ExtraNetwork
	ExtraHosts        map[string]string
	Command           []string
	Args              []string
	Env               map[string]string
	User              string
	Ports             []PortBinding
	VolumeMounts      []VolumeMount
	ConfigMounts      []ConfigMount
	HostBindMounts    []HostBindMount
	InlineFiles       map[string]string
	Privileged        bool
	Capabilities      []string
	ShmSize           string
	GPUs              string
	Running           bool
	CPULimit          string
	MemoryLimit       string
	MemoryReservation string
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
