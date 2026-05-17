package asset

type Hint struct {
	Path        string `yaml:"path" json:"path"`
	Title       string `yaml:"title,omitempty" json:"title,omitempty"`
	Description string `yaml:"description" json:"description"`
}

type Spec struct {
	Type       string            `yaml:"type" json:"type"`
	Name       string            `yaml:"name" json:"name"`
	Version    string            `yaml:"version,omitempty" json:"version,omitempty"`
	DependsOn  []string          `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	Hints      []Hint            `yaml:"hints,omitempty" json:"hints,omitempty"`
	Payload    map[string]string `yaml:"payload,omitempty" json:"payload,omitempty"`
	Properties map[string]any    `yaml:"properties,omitempty" json:"properties,omitempty"`
}

type State struct {
	Partition string            `json:"partition"`
	Intent    string            `json:"intent"`
	Asset     string            `json:"asset"`
	VersionID string            `json:"versionID"`
	Outputs   map[string]string `json:"outputs,omitempty"`
}
