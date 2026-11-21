// Package discover scans a repository for deployable workloads and produces a
// profile Forge can turn into a partition. Suggestions are evidence-based and
// never write anything.
package discover

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/repoconfig"
	"gopkg.in/yaml.v3"
)

// Profile is the discovery output.
type Profile struct {
	Root        string      `json:"root"`
	ScannedAt   time.Time   `json:"scannedAt"`
	Repository  *Repository `json:"repository,omitempty"`
	Languages   []string    `json:"languages,omitempty"`
	BuildFiles  []string    `json:"buildFiles,omitempty"`
	Workloads   []Workload  `json:"workloads"`
	Suggestions []string    `json:"suggestions,omitempty"`
}

// Repository is the repo identity found in .guardian/config.yaml, if any.
type Repository struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// Workload is one detected deployable unit.
type Workload struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Source      string   `json:"source"`
	Image       string   `json:"image,omitempty"`
	Runtime     string   `json:"runtime,omitempty"`
	Ports       []int    `json:"ports,omitempty"`
	Env         []string `json:"env,omitempty"`
	HealthPaths []string `json:"healthPaths,omitempty"`
	Evidence    []string `json:"evidence"`
	Confidence  string   `json:"confidence"`
}

// Options controls a scan.
type Options struct {
	Root     string
	MaxFiles int
}

const maxReadBytes = 2 << 20

var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, "vendor": true,
	".venv": true, "venv": true, "dist": true, "build": true, "tmp": true,
	".forge": true, "cdk.out": true, ".next": true, ".cache": true, "target": true,
}

// Run scans root and returns a profile.
func Run(ctx context.Context, opts Options) (Profile, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Profile{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Profile{}, err
	}
	if !info.IsDir() {
		return Profile{}, fmt.Errorf("%s is not a directory", root)
	}
	maxFiles := opts.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 20000
	}

	profile := Profile{Root: root, ScannedAt: time.Now().UTC(), Workloads: []Workload{}}
	if cfg, _, found, err := repoconfig.Load(root); err == nil && found {
		profile.Repository = &Repository{Name: cfg.Repo.Name, Source: cfg.Repo.Source, Branch: cfg.Repo.Branch}
	}

	walkErr := walkFiles(ctx, root, maxFiles, func(path string, rel string) {
		switch {
		case isDockerfile(filepath.Base(path)):
			if workload, ok := parseDockerfile(root, path, rel); ok {
				profile.Workloads = append(profile.Workloads, workload)
			}
		case isComposeFile(filepath.Base(path)):
			profile.Workloads = append(profile.Workloads, parseCompose(root, path, rel)...)
		case isYAML(path):
			profile.Workloads = append(profile.Workloads, parseKubernetes(path, rel)...)
		}
		switch filepath.Base(path) {
		case "go.mod":
			profile.addLanguage("go")
		case "package.json":
			profile.addLanguage("node")
		case "pyproject.toml", "requirements.txt":
			profile.addLanguage("python")
		case "Cargo.toml":
			profile.addLanguage("rust")
		case "pom.xml", "build.gradle", "build.gradle.kts":
			profile.addLanguage("java")
		}
		if isBuildFile(filepath.Base(path)) {
			profile.BuildFiles = append(profile.BuildFiles, rel)
		}
	})
	if walkErr != nil {
		return profile, walkErr
	}

	profile.Workloads = dedupeWorkloads(profile.Workloads)
	sort.Slice(profile.Workloads, func(i, j int) bool {
		if profile.Workloads[i].Name == profile.Workloads[j].Name {
			return profile.Workloads[i].Source < profile.Workloads[j].Source
		}
		return profile.Workloads[i].Name < profile.Workloads[j].Name
	})
	sort.Strings(profile.Languages)
	sort.Strings(profile.BuildFiles)
	profile.Suggestions = suggestions(profile.Workloads)
	return profile, nil
}

func (p *Profile) addLanguage(value string) {
	for _, existing := range p.Languages {
		if existing == value {
			return
		}
	}
	p.Languages = append(p.Languages, value)
}

func walkFiles(ctx context.Context, root string, maxFiles int, visit func(path, rel string)) error {
	count := 0
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[strings.ToLower(d.Name())] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		count++
		if count > maxFiles {
			return nil
		}
		visit(path, rel)
		return nil
	})
}

func isDockerfile(name string) bool {
	return name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.")
}

func isComposeFile(name string) bool {
	switch name {
	case "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
		return true
	}
	return strings.HasPrefix(name, "docker-compose.") && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml"))
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func isBuildFile(name string) bool {
	switch name {
	case "Dockerfile", "Makefile", "go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml", "build.gradle":
		return true
	}
	return false
}

var (
	exposePattern    = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(.+)$`)
	envPattern       = regexp.MustCompile(`(?i)^\s*ENV\s+([A-Za-z_][A-Za-z0-9_]*)`)
	fromPattern      = regexp.MustCompile(`(?i)^\s*FROM\s+([^\s]+)`)
	healthURLPattern = regexp.MustCompile(`https?://[^\s"']+`)
)

func parseDockerfile(root, path, rel string) (Workload, bool) {
	data, err := readCapped(path)
	if err != nil {
		return Workload{}, false
	}
	workload := Workload{
		Name:       dockerfileWorkloadName(root, path),
		Kind:       "dockerfile",
		Source:     rel,
		Confidence: "high",
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		evidence := func(text string) {
			workload.Evidence = append(workload.Evidence, fmt.Sprintf("%s:%d %s", rel, i+1, strings.TrimSpace(text)))
		}
		if match := fromPattern.FindStringSubmatch(line); len(match) == 2 {
			workload.Runtime = runtimeFromImage(match[1])
			evidence(line)
		}
		if match := exposePattern.FindStringSubmatch(line); len(match) == 2 {
			for _, field := range strings.Fields(match[1]) {
				port := strings.SplitN(field, "/", 2)[0]
				if value, err := strconv.Atoi(port); err == nil {
					workload.Ports = append(workload.Ports, value)
				}
			}
			evidence(line)
		}
		if match := envPattern.FindStringSubmatch(line); len(match) == 2 {
			workload.Env = append(workload.Env, match[1])
			evidence(line)
		}
		if strings.Contains(strings.ToUpper(line), "HEALTHCHECK") {
			if urls := healthURLPattern.FindAllString(line, -1); len(urls) > 0 {
				for _, raw := range urls {
					if path := urlPath(raw); path != "" {
						workload.HealthPaths = append(workload.HealthPaths, path)
					}
				}
			}
			evidence(line)
		}
	}
	if len(workload.Evidence) == 0 {
		workload.Evidence = append(workload.Evidence, rel)
		workload.Confidence = "medium"
	}
	workload.Ports = uniqueInts(workload.Ports)
	workload.Env = uniqueStrings(workload.Env)
	workload.HealthPaths = uniqueStrings(workload.HealthPaths)
	return workload, true
}

func dockerfileWorkloadName(root, path string) string {
	dir := filepath.Dir(path)
	if filepath.Clean(dir) == filepath.Clean(root) {
		return filepath.Base(root)
	}
	return filepath.Base(dir)
}

func runtimeFromImage(image string) string {
	image = strings.ToLower(image)
	switch {
	case strings.Contains(image, "golang"), strings.Contains(image, "go:"):
		return "go"
	case strings.Contains(image, "python"):
		return "python"
	case strings.Contains(image, "node"), strings.Contains(image, "npm"):
		return "node"
	case strings.Contains(image, "rust"):
		return "rust"
	case strings.Contains(image, "openjdk"), strings.Contains(image, "java"):
		return "java"
	case strings.Contains(image, "nginx"):
		return "nginx"
	default:
		return ""
	}
}

func parseCompose(root, path, rel string) []Workload {
	data, err := readCapped(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Image       string `yaml:"image"`
			Build       any    `yaml:"build"`
			Ports       []any  `yaml:"ports"`
			Environment any    `yaml:"environment"`
			Healthcheck struct {
				Test any `yaml:"test"`
			} `yaml:"healthcheck"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Services) == 0 {
		return nil
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Workload, 0, len(names))
	for _, name := range names {
		service := doc.Services[name]
		workload := Workload{
			Name:       name,
			Kind:       "compose",
			Source:     rel,
			Image:      strings.TrimSpace(service.Image),
			Confidence: "high",
		}
		workload.Runtime = runtimeFromImage(workload.Image)
		workload.Evidence = append(workload.Evidence, fmt.Sprintf("%s service %s", rel, name))
		if service.Image != "" {
			workload.Evidence = append(workload.Evidence, fmt.Sprintf("%s service %s image %s", rel, name, service.Image))
		}
		for _, raw := range service.Ports {
			if port := composePort(raw); port > 0 {
				workload.Ports = append(workload.Ports, port)
			}
		}
		workload.Env = composeEnv(service.Environment)
		workload.HealthPaths = composeHealthPaths(service.Healthcheck.Test)
		workload.Ports = uniqueInts(workload.Ports)
		workload.Env = uniqueStrings(workload.Env)
		out = append(out, workload)
	}
	return out
}

func composePort(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case string:
		parts := strings.Split(v, ":")
		port := strings.SplitN(parts[len(parts)-1], "/", 2)[0]
		if parsed, err := strconv.Atoi(strings.TrimSpace(port)); err == nil {
			return parsed
		}
	}
	return 0
}

func composeEnv(value any) []string {
	switch v := value.(type) {
	case map[string]any:
		out := make([]string, 0, len(v))
		for key := range v {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				if idx := strings.Index(text, "="); idx > 0 {
					out = append(out, text[:idx])
				} else if text != "" {
					out = append(out, text)
				}
			}
		}
		return out
	}
	return nil
}

func composeHealthPaths(test any) []string {
	var parts []string
	switch v := test.(type) {
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
	case string:
		parts = append(parts, v)
	}
	out := make([]string, 0)
	for _, part := range parts {
		for _, raw := range healthURLPattern.FindAllString(part, -1) {
			if path := urlPath(raw); path != "" {
				out = append(out, path)
			}
		}
	}
	return uniqueStrings(out)
}

func parseKubernetes(path, rel string) []Workload {
	data, err := readCapped(path)
	if err != nil || !strings.Contains(string(data), "kind:") {
		return nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	out := []Workload{}
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		kind, _ := doc["kind"].(string)
		switch kind {
		case "Deployment", "StatefulSet", "DaemonSet", "Pod", "Job", "CronJob":
		default:
			continue
		}
		metadata, _ := doc["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		if name == "" {
			continue
		}
		spec, _ := doc["spec"].(map[string]any)
		if kind == "CronJob" {
			if jobSpec, ok := spec["jobTemplate"].(map[string]any); ok {
				if jobTemplate, ok := jobSpec["spec"].(map[string]any); ok {
					spec = jobTemplate
				}
			}
		}
		var podSpec map[string]any
		if template, ok := spec["template"].(map[string]any); ok {
			podSpec, _ = template["spec"].(map[string]any)
		}
		if podSpec == nil {
			podSpec = spec
		}
		containers, _ := podSpec["containers"].([]any)
		workload := Workload{
			Name:       name,
			Kind:       "kubernetes",
			Source:     rel,
			Confidence: "high",
			Evidence:   []string{fmt.Sprintf("%s %s/%s", rel, kind, name)},
		}
		for _, raw := range containers {
			container, _ := raw.(map[string]any)
			if container == nil {
				continue
			}
			if image, _ := container["image"].(string); image != "" && workload.Image == "" {
				workload.Image = image
				workload.Runtime = runtimeFromImage(image)
			}
			if ports, ok := container["ports"].([]any); ok {
				for _, rawPort := range ports {
					port, _ := rawPort.(map[string]any)
					if value, ok := toInt(port["containerPort"]); ok {
						workload.Ports = append(workload.Ports, value)
					}
				}
			}
			if env, ok := container["env"].([]any); ok {
				for _, rawEnv := range env {
					entry, _ := rawEnv.(map[string]any)
					if name, _ := entry["name"].(string); name != "" {
						workload.Env = append(workload.Env, name)
					}
				}
			}
			for _, probeName := range []string{"readinessProbe", "livenessProbe", "startupProbe"} {
				probe, _ := container[probeName].(map[string]any)
				if probe == nil {
					continue
				}
				httpGet, _ := probe["httpGet"].(map[string]any)
				if path, _ := httpGet["path"].(string); path != "" {
					workload.HealthPaths = append(workload.HealthPaths, path)
				}
			}
		}
		workload.Ports = uniqueInts(workload.Ports)
		workload.Env = uniqueStrings(workload.Env)
		workload.HealthPaths = uniqueStrings(workload.HealthPaths)
		out = append(out, workload)
	}
	return out
}

func toInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(v)
		return parsed, err == nil
	}
	return 0, false
}

func readCapped(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxReadBytes {
		return nil, fmt.Errorf("file too large")
	}
	return os.ReadFile(path)
}

func urlPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "://"); idx >= 0 {
		raw = raw[idx+3:]
	}
	if idx := strings.Index(raw, "/"); idx >= 0 {
		path := raw[idx:]
		if cut := strings.IndexAny(path, "?\"' "); cut >= 0 {
			path = path[:cut]
		}
		if path != "" {
			return path
		}
	}
	return ""
}

func uniqueInts(values []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Ints(out)
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func dedupeWorkloads(values []Workload) []Workload {
	seen := map[string]bool{}
	out := make([]Workload, 0, len(values))
	for _, value := range values {
		key := value.Kind + "|" + value.Name + "|" + value.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func suggestions(workloads []Workload) []string {
	out := make([]string, 0, len(workloads))
	for _, workload := range workloads {
		out = append(out, fmt.Sprintf("create a partition from %s and add a Compute asset for %q", workload.Source, workload.Name))
	}
	return out
}
