// Package bootstrap provides the environment and logic for bootstrapping
// the MonoFS + Guardian + LB-edge stack into a Kubernetes cluster.
// This is the ONLY part that cannot go through Guardian itself.
package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the parsed bootstrap.yaml configuration.
type Config struct {
	Storage  StorageConfig  `yaml:"storage"`
	LB       LBConfig       `yaml:"lb"`
	Guardian GuardianConfig `yaml:"guardian"`
}

type StorageConfig struct {
	Namespace    string   `yaml:"namespace"`
	ClusterID    string   `yaml:"clusterId"`
	NodeNames    []string `yaml:"nodeNames"`
	RouterSuffixes []string `yaml:"routerSuffixes"`

	Images StorageImages `yaml:"images"`
	PVC    StoragePVC    `yaml:"pvc"`
	OTel   StorageOTel   `yaml:"openTelemetry"`

	External StorageExternal `yaml:"external"`
	Registry StorageRegistry `yaml:"registry"`

	NodeExternalPorts       map[string]string `yaml:"nodeExternalPorts"`
	SearchDiagnosticsAddr   string            `yaml:"searchDiagnosticsAddr"`
	FetcherDiagnosticsAddrs string            `yaml:"fetcherDiagnosticsAddrs"`
	UserServicePorts        []int             `yaml:"userServicePorts"`
}

type StorageImages struct {
	Server     string `yaml:"server"`
	Router     string `yaml:"router"`
	Fetcher    string `yaml:"fetcher"`
	Search     string `yaml:"search"`
	Registry   string `yaml:"registry"`
	LB         string `yaml:"lb"`
	Minio      string `yaml:"minio"`
	PullPolicy string `yaml:"pullPolicy"`
}

type StoragePVC struct {
	MinioSize   string `yaml:"minioSize"`
	FetcherSize string `yaml:"fetcherSize"`
	SearchSize  string `yaml:"searchSize"`
	NodeSize    string `yaml:"nodeSize"`
}

type StorageOTel struct {
	Endpoint       string `yaml:"endpoint"`
	Insecure       string `yaml:"insecure"`
	ServiceName    string `yaml:"serviceName"`
	MetricInterval string `yaml:"metricInterval"`
}

type StorageExternal struct {
	ServiceType     string `yaml:"serviceType"`
	AddressTemplate string `yaml:"addressTemplate"`
	LBPortMin       int    `yaml:"lbPortMin"`
	LBPortMax       int    `yaml:"lbPortMax"`
}

type StorageRegistry struct {
	DefaultUpstream string `yaml:"defaultUpstream"`
	Upstreams       string `yaml:"upstreams"`
}

type LBConfig struct {
	Namespace         string `yaml:"namespace"`
	PinToControlPlane bool   `yaml:"pinToControlPlane"`
}

type GuardianConfig struct {
	Namespace string `yaml:"namespace"`
	UIPort    string `yaml:"uiPort"`
	UIListen  string `yaml:"uiListen"`
	UIBaseURL string `yaml:"uiBaseURL"`

	Images        GuardianImages        `yaml:"images"`
	Monofs        GuardianMonofs        `yaml:"monofs"`
	Pushers       GuardianPushers       `yaml:"pushers"`
	ImageBuild    GuardianImageBuild    `yaml:"imageBuild"`
	LocalRegistry GuardianLocalRegistry `yaml:"localRegistry"`
	PortForward   GuardianPortForward   `yaml:"portForward"`
}

type GuardianImages struct {
	Guardiand   string `yaml:"guardiand"`
	PusherK8s   string `yaml:"pusherK8s"`
	PusherAws   string `yaml:"pusherAws"`
	PusherDocker string `yaml:"pusherDocker"`
	LB          string `yaml:"lb"`
	PullPolicy  string `yaml:"pullPolicy"`
}

type GuardianMonofs struct {
	Router                     string `yaml:"router"`
	UseExternalAddresses       string `yaml:"useExternalAddresses"`
	ClientUseExternalAddresses string `yaml:"clientUseExternalAddresses"`
}

type GuardianPushers struct {
	K8s    GuardianPusherK8s    `yaml:"k8s"`
	Docker GuardianPusherDocker `yaml:"docker"`
	AWS    GuardianPusherAWS    `yaml:"aws"`
}

type GuardianPusherK8s struct {
	Name    string `yaml:"name"`
	Cluster string `yaml:"cluster"`
}

type GuardianPusherDocker struct {
	Name string `yaml:"name"`
}

type GuardianPusherAWS struct {
	Enabled         bool   `yaml:"enabled"`
	Account         string `yaml:"account"`
	Region          string `yaml:"region"`
	AssumeRoleName  string `yaml:"assumeRoleName"`
}

type GuardianImageBuild struct {
	Registry             string `yaml:"registry"`
	KanikoMirror         string `yaml:"kanikoMirror"`
	KanikoDockerConfigSecret string `yaml:"kanikoDockerConfigSecret"`
}

type GuardianLocalRegistry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Host      string `yaml:"host"`
	Port      string `yaml:"port"`
}

type GuardianPortForward struct {
	HTTP    int    `yaml:"http"`
	GRPC    int    `yaml:"grpc"`
	UI      int    `yaml:"ui"`
	Admin   int    `yaml:"admin"`
	Address string `yaml:"address"`
}

// Env holds all computed environment variables for template rendering.
type Env map[string]string

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Storage: StorageConfig{
			Namespace:    "monofs",
			ClusterID:    "monofs-cluster",
			NodeNames:    []string{"node-a", "node-b", "node-c", "node-d", "node-e"},
			RouterSuffixes: []string{"a", "b"},
			Images: StorageImages{
				Server:     "monofs-server:latest",
				Router:     "monofs-router:latest",
				Fetcher:    "monofs-fetcher:latest",
				Search:     "monofs-search:latest",
				Registry:   "monofs-registry:latest",
				LB:         "lb:latest",
				Minio:      "mirror.gcr.io/minio/minio:latest",
				PullPolicy: "IfNotPresent",
			},
			PVC: StoragePVC{
				MinioSize:   "50Gi",
				FetcherSize: "20Gi",
				SearchSize:  "40Gi",
				NodeSize:    "100Gi",
			},
			OTel: StorageOTel{
				Endpoint:       "",
				Insecure:       "true",
				ServiceName:    "monofs-server",
				MetricInterval: "30s",
			},
			External: StorageExternal{
				ServiceType:     "LoadBalancer",
				AddressTemplate: "",
				LBPortMin:       30000,
				LBPortMax:       32767,
			},
			Registry: StorageRegistry{
				DefaultUpstream: "https://registry-1.docker.io",
				Upstreams:       "",
			},
			NodeExternalPorts: map[string]string{
				"node-a": "9006",
				"node-b": "9002",
				"node-c": "9003",
				"node-d": "9004",
				"node-e": "9005",
			},
			SearchDiagnosticsAddr:   "search-index:9101",
			FetcherDiagnosticsAddrs: "fetcher-a:9201,fetcher-b:9201",
			UserServicePorts:        []int{9191, 8888},
		},
		LB: LBConfig{
			Namespace:         "lb-edge",
			PinToControlPlane: true,
		},
		Guardian: GuardianConfig{
			Namespace: "guardian",
			UIPort:    "8090",
			UIListen:  ":8090",
			UIBaseURL: "",
			Images: GuardianImages{
				Guardiand:    "guardian:latest",
				PusherK8s:    "guardian-pusher-k8s:latest",
				PusherAws:    "guardian-pusher-aws:latest",
				PusherDocker: "guardian-pusher-docker:latest",
				LB:           "lb:latest",
				PullPolicy:   "IfNotPresent",
			},
			Monofs: GuardianMonofs{
				Router:                     "monofs-external.lb-edge.svc.cluster.local:9090",
				UseExternalAddresses:       "false",
				ClientUseExternalAddresses: "true",
			},
			Pushers: GuardianPushers{
				K8s:    GuardianPusherK8s{Name: "k8s-main", Cluster: "k8s-main"},
				Docker: GuardianPusherDocker{Name: "docker-main"},
				AWS: GuardianPusherAWS{
					Enabled:        false,
					Account:        "",
					Region:         "us-east-1",
					AssumeRoleName: "GuardianCdkDeployRole",
				},
			},
			ImageBuild: GuardianImageBuild{
				Registry:                 "registry.strata.local:5000",
				KanikoMirror:             "registry.strata.local:5000",
				KanikoDockerConfigSecret: "",
			},
			LocalRegistry: GuardianLocalRegistry{
				Name:      "monofs-registry",
				Namespace: "monofs",
				Host:      "registry.strata.local",
				Port:      "5000",
			},
			PortForward: GuardianPortForward{
				HTTP:    8080,
				GRPC:    9090,
				UI:      8090,
				Admin:   18081,
				Address: "0.0.0.0",
			},
		},
	}
}

// LoadConfig reads bootstrap.yaml, merges with defaults, applies env overrides.
func LoadConfig(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	if configPath == "" {
		// No config path given — find it automatically
		configPath = findBootstrapYAML()
	}

	data, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults if no config file
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading bootstrap config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing bootstrap config: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	return &cfg, nil
}

// findBootstrapYAML searches for bootstrap.yaml in common locations.
func findBootstrapYAML() string {
	candidates := []string{
		"deploy/bootstrap/bootstrap.yaml",
		"../stratatools/deploy/bootstrap/bootstrap.yaml",
		filepath.Join(findRoot(), "deploy", "bootstrap", "bootstrap.yaml"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Return the first candidate — will fail gracefully if not found
	return candidates[0]
}

// applyEnvOverrides applies environment variable overrides on top of config file.
// This follows the same naming convention as the Python code.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MONOFS_NAMESPACE"); v != "" {
		cfg.Storage.Namespace = v
	}
	if v := os.Getenv("MONOFS_CLUSTER_ID"); v != "" {
		cfg.Storage.ClusterID = v
	}
	if v := os.Getenv("MONOFS_SERVER_IMAGE"); v != "" {
		cfg.Storage.Images.Server = v
	}
	if v := os.Getenv("MONOFS_ROUTER_IMAGE"); v != "" {
		cfg.Storage.Images.Router = v
	}
	if v := os.Getenv("MONOFS_FETCHER_IMAGE"); v != "" {
		cfg.Storage.Images.Fetcher = v
	}
	if v := os.Getenv("MONOFS_SEARCH_IMAGE"); v != "" {
		cfg.Storage.Images.Search = v
	}
	if v := os.Getenv("MONOFS_REGISTRY_IMAGE"); v != "" {
		cfg.Storage.Images.Registry = v
	}
	if v := os.Getenv("MONOFS_LB_IMAGE"); v != "" {
		cfg.Storage.Images.LB = v
	}
	if v := os.Getenv("MINIO_IMAGE"); v != "" {
		cfg.Storage.Images.Minio = v
	}
	if v := os.Getenv("MONOFS_IMAGE_PULL_POLICY"); v != "" {
		cfg.Storage.Images.PullPolicy = v
	}
	if v := os.Getenv("MINIO_PVC_SIZE"); v != "" {
		cfg.Storage.PVC.MinioSize = v
	}
	if v := os.Getenv("FETCHER_PVC_SIZE"); v != "" {
		cfg.Storage.PVC.FetcherSize = v
	}
	if v := os.Getenv("SEARCH_PVC_SIZE"); v != "" {
		cfg.Storage.PVC.SearchSize = v
	}
	if v := os.Getenv("NODE_PVC_SIZE"); v != "" {
		cfg.Storage.PVC.NodeSize = v
	}
	if v := os.Getenv("MONOFS_OTEL_ENDPOINT"); v != "" {
		cfg.Storage.OTel.Endpoint = v
	}
	if v := os.Getenv("MONOFS_OTEL_INSECURE"); v != "" {
		cfg.Storage.OTel.Insecure = v
	}
	if v := os.Getenv("MONOFS_OTEL_SERVICE_NAME"); v != "" {
		cfg.Storage.OTel.ServiceName = v
	}
	if v := os.Getenv("MONOFS_OTEL_METRIC_INTERVAL"); v != "" {
		cfg.Storage.OTel.MetricInterval = v
	}
	if v := os.Getenv("EXTERNAL_SERVICE_TYPE"); v != "" {
		cfg.Storage.External.ServiceType = v
	}
	if v := os.Getenv("LB_NAMESPACE"); v != "" {
		cfg.LB.Namespace = v
	}
	if v := os.Getenv("GUARDIAN_NAMESPACE"); v != "" {
		cfg.Guardian.Namespace = v
	}
	if v := os.Getenv("GUARDIAN_IMAGE"); v != "" {
		cfg.Guardian.Images.Guardiand = v
	}
	if v := os.Getenv("GUARDIAN_IMAGE_PULL_POLICY"); v != "" {
		cfg.Guardian.Images.PullPolicy = v
	}
	if v := os.Getenv("GUARDIAN_PUSHER_IMAGE"); v != "" {
		cfg.Guardian.Images.PusherK8s = v
	}
	if v := os.Getenv("GUARDIAN_PUSHER_AWS_IMAGE"); v != "" {
		cfg.Guardian.Images.PusherAws = v
	}
	if v := os.Getenv("GUARDIAN_LB_IMAGE"); v != "" {
		cfg.Guardian.Images.LB = v
	}
	if v := os.Getenv("GUARDIAN_MONOFS_ROUTER"); v != "" {
		cfg.Guardian.Monofs.Router = v
	}
	if v := os.Getenv("GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES"); v != "" {
		cfg.Guardian.Monofs.UseExternalAddresses = v
	}
	if v := os.Getenv("GUARDIAN_MONOFS_CLIENT_USE_EXTERNAL_ADDRESSES"); v != "" {
		cfg.Guardian.Monofs.ClientUseExternalAddresses = v
	}
	if v := os.Getenv("GUARDIAN_PUSHER_NAME"); v != "" {
		cfg.Guardian.Pushers.K8s.Name = v
	}
	if v := os.Getenv("GUARDIAN_CLUSTER"); v != "" {
		cfg.Guardian.Pushers.K8s.Cluster = v
	}
	if v := os.Getenv("GUARDIAN_AWS_ACCOUNT"); v != "" {
		cfg.Guardian.Pushers.AWS.Enabled = true
		cfg.Guardian.Pushers.AWS.Account = v
	}
	if v := os.Getenv("GUARDIAN_AWS_REGION"); v != "" {
		cfg.Guardian.Pushers.AWS.Region = v
	}
	if v := os.Getenv("GUARDIAN_AWS_ASSUME_ROLE_NAME"); v != "" {
		cfg.Guardian.Pushers.AWS.AssumeRoleName = v
	}
	if v := os.Getenv("GUARDIAN_IMAGE_BUILD_REGISTRY"); v != "" {
		cfg.Guardian.ImageBuild.Registry = v
	}
	if v := os.Getenv("GUARDIAN_KANIKO_REGISTRY_MIRROR"); v != "" {
		cfg.Guardian.ImageBuild.KanikoMirror = v
	}
	if v := os.Getenv("LOCAL_REGISTRY_HOST"); v != "" {
		cfg.Guardian.LocalRegistry.Host = v
	}
	if v := os.Getenv("LOCAL_REGISTRY_PORT"); v != "" {
		cfg.Guardian.LocalRegistry.Port = v
	}
	if v := os.Getenv("GUARDIAN_UI_PORT"); v != "" {
		cfg.Guardian.UIPort = v
	}
	if v := os.Getenv("GUARDIAN_UI_LISTEN"); v != "" {
		cfg.Guardian.UIListen = v
	}
	if v := os.Getenv("GUARDIAN_UI_BASE_URL"); v != "" {
		cfg.Guardian.UIBaseURL = v
	}
	if v := os.Getenv("GUARDIAN_DOCKER_PUSHER_NAME"); v != "" {
		cfg.Guardian.Pushers.Docker.Name = v
	}
	if v := os.Getenv("GUARDIAN_DOCKER_PUSHER_IMAGE"); v != "" {
		cfg.Guardian.Images.PusherDocker = v
	}
}

// TemplateDirs returns the storage and guardian template directories.
// Templates are expected at <root>/deploy/bootstrap/storage/ and
// <root>/deploy/bootstrap/guardian/ relative to the stratatools repo root.
func TemplateDirs() (storageDir, guardianDir string) {
	root := findRoot()
	return filepath.Join(root, "deploy", "bootstrap", "storage"),
		filepath.Join(root, "deploy", "bootstrap", "guardian")
}

// findRoot locates the stratatools repo root by looking for deploy/bootstrap/
// or pyproject.toml markers.
func findRoot() string {
	// Try ST_ROOT env var first
	if r := os.Getenv("ST_ROOT"); r != "" {
		return r
	}
	// Walk up from cwd
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "deploy", "bootstrap")); err == nil {
			return dir
		}
		// Check nested patterns
		for _, sub := range []string{"strata/stratatools", "projects/stratatools", "src/stratatools"} {
			if _, err := os.Stat(filepath.Join(dir, sub, "pyproject.toml")); err == nil {
				return filepath.Join(dir, sub)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "stratatools", "pyproject.toml")); err == nil {
			return filepath.Join(dir, "stratatools")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: assume cwd
	dir, _ = os.Getwd()
	return dir
}

// Run is a helper that executes a command and prints it.
// Returns nil on success. Dry-run mode prints the command and returns nil.
func Run(ctx context.Context, dryRun bool, name string, args ...string) error {
	fmt.Fprintf(os.Stderr, "+ %s %s\n", name, strings.Join(args, " "))
	if dryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunCapture runs a command and captures stdout.
func RunCapture(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Kubectl runs a kubectl command with the given args.
func Kubectl(ctx context.Context, dryRun bool, args ...string) error {
	return Run(ctx, dryRun, "kubectl", args...)
}

// KubectlCapture runs kubectl and captures stdout.
func KubectlCapture(ctx context.Context, args ...string) (string, error) {
	return RunCapture(ctx, "kubectl", args...)
}
