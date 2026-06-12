package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MonoFS     MonoFSConfig     `yaml:"monofs"`
	KVS        KVSConfig        `yaml:"kvs"`
	Guardian   GuardianConfig   `yaml:"guardian"`
	Compliance ComplianceConfig `yaml:"compliance"`
	Pushers    []PusherConfig   `yaml:"pushers"`
}

type MonoFSConfig struct {
	MountPath                  string `yaml:"mountPath"`
	APIEndpoint                string `yaml:"apiEndpoint"`
	ClientAPIEndpoint          string `yaml:"clientApiEndpoint"`
	Token                      string `yaml:"token"`
	PrincipalID                string `yaml:"principalID"`
	UseExternalAddresses       bool   `yaml:"useExternalAddresses"`
	ClientUseExternalAddresses *bool  `yaml:"clientUseExternalAddresses"`
}

func (c MonoFSConfig) DiscoveryUseExternalAddresses() bool {
	if c.ClientUseExternalAddresses != nil {
		return *c.ClientUseExternalAddresses
	}
	return c.UseExternalAddresses
}

type KVSConfig struct {
	APIEndpoint string `yaml:"apiEndpoint"`
}

type GuardianConfig struct {
	PrincipalID          string `yaml:"principalID"`
	ReconcileInterval    string `yaml:"reconcileInterval"`
	DebounceMs           int    `yaml:"debounceMs"`
	UIListenAddress      string `yaml:"uiListenAddress"`
	UIBaseURL            string `yaml:"uiBaseURL"`
	ClientDiscoveryToken string `yaml:"clientDiscoveryToken"`
	StaleTaskAfter       string `yaml:"staleTaskAfter"`
}

type ComplianceConfig struct {
	S3Bucket       string `yaml:"s3Bucket"`
	S3Region       string `yaml:"s3Region"`
	S3Prefix       string `yaml:"s3Prefix"`
	S3Endpoint     string `yaml:"s3Endpoint"`
	ForcePathStyle bool   `yaml:"forcePathStyle"`
}

type PusherConfig struct {
	Name     string `yaml:"name"`
	QueueDir string `yaml:"queueDir"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Default() *Config {
	return &Config{
		MonoFS: MonoFSConfig{
			PrincipalID: "guardian",
		},
		KVS: KVSConfig{},
		Guardian: GuardianConfig{
			PrincipalID:          "guardian",
			ReconcileInterval:    "10m",
			DebounceMs:           250,
			UIListenAddress:      "",
			UIBaseURL:            "",
			ClientDiscoveryToken: "",
			StaleTaskAfter:       "30m",
		},
		Compliance: ComplianceConfig{},
		Pushers: []PusherConfig{{
			Name:     "local",
			QueueDir: "/.queues/local",
		}},
	}
}
