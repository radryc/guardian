// Package opentofu runs OpenTofu for the GCP pusher.
//
// Every intent gets an isolated workspace under WorkRoot, its own GCS state
// object, and an exclusive flock. The package is deliberately free of GCP SDK
// dependencies: it shells out to the pinned tofu binary and parses its JSON
// output, which keeps the diff/apply contract identical to the AWS CDK backend.
package opentofu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	defaultBinary   = "tofu"
	defaultWorkRoot = "/var/lib/guardian/pusher-gcp"
	defaultPrefix   = "guardian"
	variablesFile   = "terraform.tfvars.json"
	backendFile     = "_guardian_backend.tf"
	lockFileName    = ".guardian.lock"
)

// ErrLocked reports that another task currently owns the intent workspace.
var ErrLocked = errors.New("workspace is locked by another task")

// Config describes the OpenTofu execution environment.
type Config struct {
	// Binary is the tofu executable. Defaults to "tofu".
	Binary string
	// WorkRoot is the parent directory for per-intent workspaces.
	WorkRoot string
	// StateBucket is the GCS bucket backing every workspace.
	StateBucket string
	// StatePrefixRoot is the object prefix; each workspace appends
	// "<partition>/<intent>".
	StatePrefixRoot string
	// StateKMSKey enables OpenTofu native state encryption when set. Accepts
	// the full resource name
	// projects/<p>/locations/<r>/keyRings/<ring>/cryptoKeys/<key>.
	StateKMSKey string
	// Env is appended to every tofu invocation.
	Env map[string]string
	// KeepWorkspace keeps the workspace directory after Close. Tests set it to
	// true to inspect generated files.
	KeepWorkspace bool
}

// Backend owns tofu configuration and workspace lifecycle.
type Backend struct {
	cfg    Config
	runner Runner
}

// New validates cfg and returns a backend using the real exec runner.
func New(cfg Config) (*Backend, error) { return NewWithRunner(cfg, execRunner{}) }

// NewWithRunner is New with an injectable runner.
func NewWithRunner(cfg Config, runner Runner) (*Backend, error) {
	if runner == nil {
		return nil, errors.New("opentofu: runner is required")
	}
	if cfg.Binary == "" {
		cfg.Binary = defaultBinary
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = defaultWorkRoot
	}
	if cfg.StatePrefixRoot == "" {
		cfg.StatePrefixRoot = defaultPrefix
	}
	cfg.StatePrefixRoot = strings.Trim(cfg.StatePrefixRoot, "/")
	// Share downloaded providers across per-asset workspaces. Without a plugin
	// cache every tofu init re-downloads the Google provider.
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if _, ok := cfg.Env["TF_PLUGIN_CACHE_DIR"]; !ok {
		cacheDir := filepath.Join(cfg.WorkRoot, ".plugin-cache")
		if err := os.MkdirAll(cacheDir, 0o700); err == nil {
			cfg.Env["TF_PLUGIN_CACHE_DIR"] = cacheDir
			cfg.Env["TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE"] = "true"
		}
	}
	return &Backend{cfg: cfg, runner: runner}, nil
}

// Config returns a copy of the backend configuration.
func (b *Backend) Config() Config {
	cfg := b.cfg
	cfg.Env = cloneMap(b.cfg.Env)
	return cfg
}

// WorkspaceDir returns the directory for a partition/intent pair. When an
// asset is supplied, the directory is scoped to that asset so two assets in the
// same intent never share blueprint files or a flock.
func (b *Backend) WorkspaceDir(partition, intent string, assets ...string) string {
	parts := []string{b.cfg.WorkRoot, safeSegment(partition), safeSegment(intent)}
	if asset := firstAsset(assets); asset != "" {
		parts = append(parts, safeSegment(asset))
	}
	return filepath.Join(parts...)
}

// StatePrefix returns the GCS object prefix for a partition/intent pair. With
// an asset it returns a per-asset prefix: an intent with several assets must
// not share one tofu state, otherwise applying one asset destroys the others.
func (b *Backend) StatePrefix(partition, intent string, assets ...string) string {
	parts := []string{b.cfg.StatePrefixRoot, safeSegment(partition), safeSegment(intent)}
	if asset := firstAsset(assets); asset != "" {
		parts = append(parts, safeSegment(asset))
	}
	return path.Join(parts...)
}

func firstAsset(assets []string) string {
	if len(assets) == 0 {
		return ""
	}
	return strings.TrimSpace(assets[0])
}

// Open creates an isolated, locked workspace for one intent (or one asset of an
// intent when an asset name is supplied).
func (b *Backend) Open(partition, intent string, assets ...string) (*Workspace, error) {
	if strings.TrimSpace(partition) == "" {
		return nil, errors.New("opentofu: partition is required")
	}
	if strings.TrimSpace(intent) == "" {
		return nil, errors.New("opentofu: intent is required")
	}
	if strings.TrimSpace(b.cfg.StateBucket) == "" {
		return nil, errors.New("opentofu: state bucket is required")
	}
	asset := firstAsset(assets)

	dir := b.WorkspaceDir(partition, intent, asset)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("opentofu: create workspace %q: %w", dir, err)
	}
	if err := os.MkdirAll(filepath.Join(b.cfg.WorkRoot, ".locks"), 0o700); err != nil {
		return nil, fmt.Errorf("opentofu: create lock directory: %w", err)
	}
	lock, err := acquireLock(b.lockPath(partition, intent, asset))
	if err != nil {
		return nil, err
	}
	return &Workspace{
		backend:   b,
		dir:       dir,
		partition: partition,
		intent:    intent,
		asset:     asset,
		lock:      lock,
	}, nil
}

// lockPath returns a stable lock file outside the workspace directory. The
// workspace is removed on Close, so keeping the flock inside it allows a
// concurrent Open to create a new lock file (and a new inode) while Close is
// still removing the old directory.
func (b *Backend) lockPath(partition, intent, asset string) string {
	name := safeSegment(partition) + "-" + safeSegment(intent)
	if asset != "" {
		name += "-" + safeSegment(asset)
	}
	return filepath.Join(b.cfg.WorkRoot, ".locks", name+".lock")
}

// safeSegment keeps partition/intent names usable as path components and GCS
// object segments without allowing traversal.
func safeSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, string(filepath.Separator), "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "..", "-")
	if value == "" {
		return "default"
	}
	return value
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// run executes the tofu binary with the workspace environment.
func (b *Backend) run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	env := make([]string, 0, len(b.cfg.Env)+2)
	env = append(env, "TF_IN_AUTOMATION=1", "TF_INPUT=0")
	for key, value := range b.cfg.Env {
		env = append(env, key+"="+value)
	}
	return b.runner.Run(ctx, dir, env, b.cfg.Binary, args...)
}
