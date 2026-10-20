package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MonoFS        MonoFSConfig        `yaml:"monofs"`
	KVS           KVSConfig           `yaml:"kvs"`
	Guardian      GuardianConfig      `yaml:"guardian"`
	Compliance    ComplianceConfig    `yaml:"compliance"`
	Pushers       []PusherConfig      `yaml:"pushers"`
	Notifications NotificationsConfig `yaml:"notifications"`
}

// NotificationsConfig configures partition failure alerting.
type NotificationsConfig struct {
	Enabled        bool                  `yaml:"enabled"`
	PollInterval   string                `yaml:"pollInterval"`
	FailThreshold  int                   `yaml:"failThreshold"`
	Cooldown       string                `yaml:"cooldown"`
	RepeatInterval string                `yaml:"repeatInterval"`
	Channels       []NotificationChannel `yaml:"channels"`
	Rules          []NotificationRule    `yaml:"rules"`
}

// NotificationChannel describes one delivery target.
type NotificationChannel struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"`
	URL            string `yaml:"url"`
	Secret         string `yaml:"secret"`
	SecretEnv      string `yaml:"secretEnv"`
	Timeout        string `yaml:"timeout"`
	Cooldown       string `yaml:"cooldown"`
	RepeatInterval string `yaml:"repeatInterval"`
}

// NotificationRule selects which events a channel receives.
type NotificationRule struct {
	Name        string   `yaml:"name"`
	Events      []string `yaml:"events"`
	MinSeverity string   `yaml:"minSeverity"`
	Channels    []string `yaml:"channels"`
}

// ChannelSecret resolves the channel signing secret.
func (c NotificationChannel) ChannelSecret() string {
	if secret := strings.TrimSpace(c.Secret); secret != "" {
		return secret
	}
	if env := strings.TrimSpace(c.SecretEnv); env != "" {
		return strings.TrimSpace(os.Getenv(env))
	}
	return ""
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
	EnableFlowTopology   bool   `yaml:"enableFlowTopology"`
	FlowMetricsURL       string `yaml:"flowMetricsURL"`
	FlowMetricsTimeout   string `yaml:"flowMetricsTimeout"`
	// ObservedInterval controls how often the observed collector polls pusher
	// observed APIs for report-only AWS intents. Empty disables the collector.
	ObservedInterval string `yaml:"observedInterval"`
	ObservedTimeout  string `yaml:"observedTimeout"`
	// ObservedDetectDrift runs CloudFormation drift detection on each observed
	// pass (billable API calls, slower). Without it only DescribeStacks state
	// is collected and out-of-band resource changes are not reported.
	ObservedDetectDrift bool `yaml:"observedDetectDrift"`
	// AutoDeployOnIngest reconciles every partition bound to a repo right after
	// that repo is ingested (UI or CLI), so a merge deploys without a manual
	// reconcile. Defaults to true; set false for manual-approval workflows.
	AutoDeployOnIngest bool `yaml:"autoDeployOnIngest"`
	// RepoImportEnabled runs a level-triggered loop that imports changed
	// guardian/partitions/** bundles from repo sources already ingested into
	// MonoFS. It closes the push -> MonoFS -> Guardian deploy loop.
	RepoImportEnabled bool `yaml:"repoImportEnabled"`
	// RepoImportInterval is how often the repo import loop scans the store.
	RepoImportInterval string `yaml:"repoImportInterval"`
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
	// ObservedAPIURL points at the pusher's read API for observed AWS intents.
	// The token is read from ObservedAPIToken or the env var named by
	// ObservedAPITokenEnv.
	ObservedAPIURL      string `yaml:"observedApiUrl"`
	ObservedAPIToken    string `yaml:"observedApiToken"`
	ObservedAPITokenEnv string `yaml:"observedApiTokenEnv"`
}

// ObservedToken resolves the bearer token for the pusher read API.
func (p PusherConfig) ObservedToken() string {
	if token := strings.TrimSpace(p.ObservedAPIToken); token != "" {
		return token
	}
	if env := strings.TrimSpace(p.ObservedAPITokenEnv); env != "" {
		return strings.TrimSpace(os.Getenv(env))
	}
	return ""
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
			EnableFlowTopology:   false,
			FlowMetricsURL:       "",
			FlowMetricsTimeout:   "2s",
			ObservedInterval:     "5m",
			ObservedTimeout:      "30s",
			ObservedDetectDrift:  false,
			AutoDeployOnIngest:   true,
			RepoImportEnabled:    true,
			RepoImportInterval:   "30s",
		},
		Compliance: ComplianceConfig{},
		Pushers: []PusherConfig{{
			Name:     "local",
			QueueDir: "/.queues/local",
		}},
	}
}
