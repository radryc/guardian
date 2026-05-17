package target

type Placement struct {
	Cluster   string `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Region    string `yaml:"region,omitempty" json:"region,omitempty"`
	Account   string `yaml:"account,omitempty" json:"account,omitempty"`
}
