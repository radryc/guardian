package assets

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type CatalogTemplate struct {
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Icon        string         `json:"icon"`
	Category    string         `json:"category"`
	Template    map[string]any `json:"template"`
	Fields      []CatalogField `json:"fields"`
	Hints       []CatalogHint  `json:"hints,omitempty"`
}

type CatalogField struct {
	Path        string   `json:"path"`
	Title       string   `json:"title"`
	Control     string   `json:"control"`
	Placeholder string   `json:"placeholder,omitempty"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"`
	RefTypes    []string `json:"refTypes,omitempty"`
}

type CatalogHint struct {
	Path        string `json:"path"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
}

var arrayIndexPattern = regexp.MustCompile(`\[\d+\]`)

var catalogTemplates = map[string]CatalogTemplate{
	assetdomain.TypeCompute: {
		Type:        assetdomain.TypeCompute,
		Title:       "Compute service",
		Description: "Run a long-lived application, worker, or daemon.",
		Icon:        "🧠",
		Category:    "Compute",
		Template: map[string]any{
			"image":    "ghcr.io/example/service:latest",
			"replicas": 1,
			"ports": []map[string]any{{
				"name":          "http",
				"containerPort": 8080,
				"protocol":      "TCP",
			}},
		},
		Fields: computeCatalogFields(),
		Hints: hints(computeCatalogFields(),
			CatalogHint{Path: "resources.requests.cpu", Title: "Requested CPU", Description: "CPU requested for scheduler placement."},
			CatalogHint{Path: "resources.requests.memory", Title: "Requested memory", Description: "Memory requested for scheduler placement."},
			CatalogHint{Path: "resources.limits.cpu", Title: "CPU limit", Description: "Maximum CPU the workload may consume."},
			CatalogHint{Path: "resources.limits.memory", Title: "Memory limit", Description: "Maximum memory the workload may consume."},
			CatalogHint{Path: "resources.reservations.cpu", Title: "Reserved CPU", Description: "CPU reserved for runtimes that support reservations."},
			CatalogHint{Path: "resources.reservations.memory", Title: "Reserved memory", Description: "Memory reserved for runtimes that support reservations."},
			CatalogHint{Path: "healthCheck.test", Title: "Health check test", Description: "Command or probe executed to determine health."},
			CatalogHint{Path: "healthCheck.interval", Title: "Health check interval", Description: "Time between health check runs."},
			CatalogHint{Path: "healthCheck.timeout", Title: "Health check timeout", Description: "Maximum time allowed for a single health check."},
			CatalogHint{Path: "healthCheck.retries", Title: "Health check retries", Description: "Consecutive failures before the workload is marked unhealthy."},
			CatalogHint{Path: "ports[].name", Title: "Port name", Description: "Logical listener name used for discovery and summaries."},
			CatalogHint{Path: "ports[].protocol", Title: "Port protocol", Description: "Transport protocol exposed by this listener."},
			CatalogHint{Path: "ports[].port", Title: "Port", Description: "Generic port number used by pushers that expect a single port field."},
			CatalogHint{Path: "ports[].containerPort", Title: "Container port", Description: "Port exposed inside the workload or container."},
			CatalogHint{Path: "ports[].hostPort", Title: "Host port", Description: "Host or node port bound directly by the workload."},
			CatalogHint{Path: "ports[].servicePort", Title: "Service port", Description: "Service-facing port exposed by the surrounding platform."},
			CatalogHint{Path: "ports[].dynamicHostname", Title: "Dynamic hostname", Description: "Hostname registered through dynamic local routing so the host-facing port does not need to be pinned manually."},
			CatalogHint{Path: "volumeMounts[].volume", Title: "Mounted volume", Description: "Referenced Volume asset name."},
			CatalogHint{Path: "volumeMounts[].path", Title: "Volume mount path", Description: "Absolute destination path inside the workload."},
			CatalogHint{Path: "volumeMounts[].readOnly", Title: "Read-only volume", Description: "Mount the referenced volume as read-only."},
			CatalogHint{Path: "configMounts[].config", Title: "Mounted config", Description: "Referenced Config asset name."},
			CatalogHint{Path: "configMounts[].path", Title: "Config mount path", Description: "Absolute destination path for rendered config files."},
			CatalogHint{Path: "configMounts[].readOnly", Title: "Read-only config", Description: "Mount rendered config files as read-only."},
			CatalogHint{Path: "hostBindMounts[].source", Title: "Host source path", Description: "Absolute host path on the pusher node."},
			CatalogHint{Path: "hostBindMounts[].target", Title: "Host bind target", Description: "Destination path inside the workload."},
			CatalogHint{Path: "hostBindMounts[].readOnly", Title: "Read-only host bind", Description: "Expose the host path to the workload as read-only."},
		),
	},
	assetdomain.TypeImageBuild: {
		Type:        assetdomain.TypeImageBuild,
		Title:       "Image build",
		Description: "Build and publish an immutable OCI image from a staged source tree.",
		Icon:        "🏗️",
		Category:    "Build",
		Template: map[string]any{
			"repository": "example-api",
			"registry":   "registry.strata.local:5000",
			"sourceDir":  "/partitions/example/payloads/sources/api",
			"dockerfile": "Dockerfile",
			"platform":   "linux/amd64",
		},
		Fields: imageBuildCatalogFields(),
		Hints: hints(imageBuildCatalogFields(),
			CatalogHint{Path: "repository", Title: "Repository name", Description: "Repository/name portion of the pushed image reference, without registry or tag."},
			CatalogHint{Path: "registry", Title: "Registry host", Description: "Registry host:port used for the published immutable image. Leave empty to let the pusher use its default registry."},
			CatalogHint{Path: "sourceDir", Title: "Staged source tree", Description: "Absolute Guardian logical path for the staged Docker build context directory."},
			CatalogHint{Path: "dockerfile", Title: "Dockerfile path", Description: "Path to the Dockerfile relative to sourceDir. Defaults to Dockerfile."},
			CatalogHint{Path: "target", Title: "Build target", Description: "Optional Dockerfile target stage to build."},
			CatalogHint{Path: "platform", Title: "Platform", Description: "Optional target platform such as linux/amd64."},
			CatalogHint{Path: "buildArgs", Title: "Build args", Description: "Docker build arguments passed to the image builder."},
		),
	},
	assetdomain.TypeCDKStack: {
		Type:        assetdomain.TypeCDKStack,
		Title:       "AWS CDK stack",
		Description: "Deploy a TypeScript AWS CDK stack as one CloudFormation stack through the AWS pusher.",
		Icon:        "☁️",
		Category:    "Cloud",
		Template: map[string]any{
			"context": map[string]any{
				"envName": "prod",
			},
			"env": map[string]any{
				"APP_NAME": "guardian-demo",
			},
		},
		Fields: cdkStackCatalogFields(),
		Hints: hints(cdkStackCatalogFields(),
			CatalogHint{Path: "payload.aws", Title: "AWS payload manifest", Description: "Absolute logical path to the AWS CDK payload manifest that points at the CDK source tree and stack metadata."},
		),
	},
	assetdomain.TypeDatabase: {
		Type:        assetdomain.TypeDatabase,
		Title:       "Database",
		Description: "Provision a SQL-compatible database service.",
		Icon:        "🗄️",
		Category:    "Storage",
		Template: map[string]any{
			"engine":   "postgres",
			"database": "app",
			"user":     "app",
			"port":     5432,
		},
		Fields: sqlDatabaseCatalogFields(),
		Hints:  hints(sqlDatabaseCatalogFields()),
	},
	assetdomain.TypeSQLDatabase: {
		Type:        assetdomain.TypeSQLDatabase,
		Title:       "SQL database",
		Description: "Explicit SQL database asset with the same schema as Database.",
		Icon:        "🗃️",
		Category:    "Storage",
		Template: map[string]any{
			"engine":   "postgres",
			"database": "app",
			"user":     "app",
			"port":     5432,
		},
		Fields: sqlDatabaseCatalogFields(),
		Hints:  hints(sqlDatabaseCatalogFields()),
	},
	assetdomain.TypeVolume: {
		Type:        assetdomain.TypeVolume,
		Title:       "Volume",
		Description: "Persistent or ephemeral storage for services.",
		Icon:        "💾",
		Category:    "Storage",
		Template: map[string]any{
			"size":       "20Gi",
			"accessMode": "ReadWriteOnce",
		},
		Fields: volumeCatalogFields(),
		Hints:  hints(volumeCatalogFields()),
	},
	assetdomain.TypeConfig: {
		Type:        assetdomain.TypeConfig,
		Title:       "Config files",
		Description: "Store mounted files or inline configuration for services.",
		Icon:        "🧾",
		Category:    "Config",
		Template: map[string]any{
			"format":  "text",
			"content": "replace-with-config",
		},
		Fields: configCatalogFields(),
		Hints:  hints(configCatalogFields()),
	},
	assetdomain.TypeNetwork: {
		Type:        assetdomain.TypeNetwork,
		Title:       "Network",
		Description: "Connectivity boundary and attachment point for workloads.",
		Icon:        "🔗",
		Category:    "Network",
		Template: map[string]any{
			"driver": "bridge",
			"scope":  "partition",
		},
		Fields: networkCatalogFields(),
		Hints:  hints(networkCatalogFields()),
	},
	assetdomain.TypeLoadBalancer: {
		Type:        assetdomain.TypeLoadBalancer,
		Title:       "Network edge",
		Description: "Expose compute services behind listeners and routing.",
		Icon:        "🌐",
		Category:    "Network",
		Template: map[string]any{
			"targets": []string{},
			"listeners": []map[string]any{{
				"name":        "http",
				"port":        8080,
				"protocol":    "http",
				"description": "HTTP API",
				// externalPort: 8080   — set to pin a fixed host port (static)
				// dynamic: true        — omit externalPort to let lb allocate a free port
			}},
		},

		Fields: loadBalancerCatalogFields(),
		Hints: hints(loadBalancerCatalogFields(),
			CatalogHint{Path: "listeners[].name", Title: "Listener name", Description: "Logical name for this public or internal listener."},
			CatalogHint{Path: "listeners[].port", Title: "Listener port", Description: "Backend port on target compute services to forward to."},
			CatalogHint{Path: "listeners[].protocol", Title: "Protocol", Description: "Transport protocol (http, grpc, tcp …) shown in lb /services."},
			CatalogHint{Path: "listeners[].description", Title: "Description", Description: "Human-readable label shown in lb /services (e.g. MonoFS HTTP UI)."},
			CatalogHint{Path: "listeners[].externalPort", Title: "External port (static)", Description: "Fixed host-facing port. Omit to let the lb allocate a free port dynamically."},
			CatalogHint{Path: "listeners[].dynamic", Title: "Dynamic allocation", Description: "When true, lb allocates a free external port. Mutually exclusive with externalPort."},
		),
	},
	assetdomain.TypeTraefikRoute: {
		Type:        assetdomain.TypeTraefikRoute,
		Title:       "Traefik route",
		Description: "Expose a local developer hostname for a target compute port through the bootstrap-managed Traefik proxy without pinning a manual localhost port.",
		Icon:        "🧭",
		Category:    "Network",
		Template: map[string]any{
			"hostname": "doctor.strata",
			"target":   "query",
			"portName": "http",
		},
		Fields: traefikRouteCatalogFields(),
		Hints: hints(traefikRouteCatalogFields(),
			CatalogHint{Path: "hostname", Title: "Hostname", Description: "Developer-facing hostname that should resolve through the local Traefik proxy."},
			CatalogHint{Path: "target", Title: "Target asset", Description: "Compute asset whose named port should be exposed locally."},
			CatalogHint{Path: "portName", Title: "Target port name", Description: "Named compute port to forward. Required when the target exposes more than one port."},
		),
	},
	assetdomain.TypeObjectStore: {
		Type:        assetdomain.TypeObjectStore,
		Title:       "Object storage",
		Description: "Provision or reference an object store with buckets and versioning.",
		Icon:        "🪣",
		Category:    "Storage",
		Template: map[string]any{
			"engine":     "minio",
			"endpoint":   "",
			"buckets":    []string{"app"},
			"versioning": true,
		},
		Fields: objectStoreCatalogFields(),
		Hints:  hints(objectStoreCatalogFields()),
	},
	assetdomain.TypeObservability: {
		Type:        assetdomain.TypeObservability,
		Title:       "Observability",
		Description: "Capture metrics, traces, or logs for the partition.",
		Icon:        "📡",
		Category:    "Operations",
		Template: map[string]any{
			"provider":  "otel",
			"receivers": []string{"otlp"},
			"exporters": []string{"otlphttp"},
		},
		Fields: observabilityCatalogFields(),
		Hints:  hints(observabilityCatalogFields()),
	},
}

func Catalog() []CatalogTemplate {
	seen := make(map[string]struct{}, len(catalogTemplates))
	items := make([]CatalogTemplate, 0, len(catalogTemplates))
	for _, assetType := range assetdomain.KnownTypes() {
		item, ok := catalogTemplates[assetType]
		if !ok {
			continue
		}
		items = append(items, item)
		seen[assetType] = struct{}{}
	}
	if len(items) == len(catalogTemplates) {
		return items
	}
	extra := make([]string, 0, len(catalogTemplates)-len(items))
	for assetType := range catalogTemplates {
		if _, ok := seen[assetType]; ok {
			continue
		}
		extra = append(extra, assetType)
	}
	sort.Strings(extra)
	for _, assetType := range extra {
		items = append(items, catalogTemplates[assetType])
	}
	return items
}

func CatalogFor(assetType string) (CatalogTemplate, bool) {
	item, ok := catalogTemplates[assetType]
	return item, ok
}

func imageBuildCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "repository", Title: "Repository", Control: "text", Placeholder: "example-api"},
		{Path: "registry", Title: "Registry", Control: "text", Placeholder: "registry.strata.local:5000"},
		{Path: "sourceDir", Title: "Source dir", Control: "text", Placeholder: "/partitions/example/payloads/sources/api"},
		{Path: "dockerfile", Title: "Dockerfile", Control: "text", Placeholder: "Dockerfile"},
		{Path: "target", Title: "Build target", Control: "text", Placeholder: "runtime"},
		{Path: "platform", Title: "Platform", Control: "text", Placeholder: "linux/amd64"},
		{Path: "buildArgs", Title: "Build args JSON", Control: "json"},
	}
}

func hints(fields []CatalogField, extra ...CatalogHint) []CatalogHint {
	items := make([]CatalogHint, 0, len(fields)+len(extra))
	seen := make(map[string]struct{}, len(fields)+len(extra))
	for _, field := range fields {
		if field.Description == "" {
			continue
		}
		items = append(items, CatalogHint{Path: field.Path, Title: field.Title, Description: field.Description})
		seen[field.Path] = struct{}{}
	}
	for _, hint := range extra {
		if _, ok := seen[hint.Path]; ok {
			continue
		}
		items = append(items, hint)
		seen[hint.Path] = struct{}{}
	}
	return items
}

func NormalizeHintPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = arrayIndexPattern.ReplaceAllString(path, "[]")
	path = strings.TrimPrefix(path, ".")
	return path
}

func ValidateAssetHints(hints []assetdomain.Hint) error {
	for idx, hint := range hints {
		path := NormalizeHintPath(hint.Path)
		if path == "" {
			return fmt.Errorf("hints[%d].path is required", idx)
		}
		if strings.TrimSpace(hint.Description) == "" {
			return fmt.Errorf("hints[%d].description is required", idx)
		}
		if strings.HasPrefix(path, "assets.") || strings.HasPrefix(path, "outputs.") {
			return fmt.Errorf("hints[%d].path must be asset-local, not %q", idx, path)
		}
	}
	return nil
}

func ValidateIntentHints(hints []assetdomain.Hint, assetNames map[string]struct{}) error {
	for idx, hint := range hints {
		path := NormalizeHintPath(hint.Path)
		if path == "" {
			return fmt.Errorf("hints[%d].path is required", idx)
		}
		if strings.TrimSpace(hint.Description) == "" {
			return fmt.Errorf("hints[%d].description is required", idx)
		}
		switch {
		case strings.HasPrefix(path, "outputs."):
			if strings.TrimSpace(strings.TrimPrefix(path, "outputs.")) == "" {
				return fmt.Errorf("hints[%d].path must target outputs.<key>", idx)
			}
		case strings.HasPrefix(path, "assets."):
			remainder := strings.TrimPrefix(path, "assets.")
			parts := strings.SplitN(remainder, ".", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return fmt.Errorf("hints[%d].path must target assets.<asset>.<field>", idx)
			}
			if _, ok := assetNames[parts[0]]; !ok {
				return fmt.Errorf("hints[%d].path references unknown asset %q", idx, parts[0])
			}
		default:
			return fmt.Errorf("hints[%d].path must start with outputs. or assets.<asset>.", idx)
		}
	}
	return nil
}

func ResolveAssetHints(assetType, assetName string, assetHints, intentHints []assetdomain.Hint) []CatalogHint {
	merged := cloneCatalogHints(defaultHintsForType(assetType))
	merged = mergeCatalogHints(merged, scopedIntentAssetHints(assetName, intentHints))
	merged = mergeCatalogHints(merged, normalizeManifestHints(assetHints))
	return merged
}

func ResolveIntentOutputHints(intentHints []assetdomain.Hint) []CatalogHint {
	outputHints := make([]CatalogHint, 0, len(intentHints))
	for _, hint := range intentHints {
		path := NormalizeHintPath(hint.Path)
		if !strings.HasPrefix(path, "outputs.") {
			continue
		}
		outputHints = append(outputHints, CatalogHint{Path: path, Title: hint.Title, Description: hint.Description})
	}
	return mergeCatalogHints(nil, outputHints)
}

func defaultHintsForType(assetType string) []CatalogHint {
	item, ok := CatalogFor(assetType)
	if !ok {
		return nil
	}
	return item.Hints
}

func scopedIntentAssetHints(assetName string, intentHints []assetdomain.Hint) []CatalogHint {
	prefix := "assets." + assetName + "."
	items := make([]CatalogHint, 0, len(intentHints))
	for _, hint := range intentHints {
		path := NormalizeHintPath(hint.Path)
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		items = append(items, CatalogHint{
			Path:        strings.TrimPrefix(path, prefix),
			Title:       hint.Title,
			Description: hint.Description,
		})
	}
	return items
}

func normalizeManifestHints(hints []assetdomain.Hint) []CatalogHint {
	items := make([]CatalogHint, 0, len(hints))
	for _, hint := range hints {
		items = append(items, CatalogHint{
			Path:        NormalizeHintPath(hint.Path),
			Title:       hint.Title,
			Description: hint.Description,
		})
	}
	return items
}

func mergeCatalogHints(base []CatalogHint, overlays ...[]CatalogHint) []CatalogHint {
	merged := cloneCatalogHints(base)
	indexByPath := make(map[string]int, len(merged))
	for idx, hint := range merged {
		indexByPath[NormalizeHintPath(hint.Path)] = idx
	}
	for _, overlay := range overlays {
		for _, hint := range overlay {
			normalized := NormalizeHintPath(hint.Path)
			if normalized == "" {
				continue
			}
			hint.Path = normalized
			if idx, ok := indexByPath[normalized]; ok {
				merged[idx] = hint
				continue
			}
			indexByPath[normalized] = len(merged)
			merged = append(merged, hint)
		}
	}
	return merged
}

func cloneCatalogHints(hints []CatalogHint) []CatalogHint {
	if len(hints) == 0 {
		return nil
	}
	cloned := make([]CatalogHint, len(hints))
	copy(cloned, hints)
	for idx := range cloned {
		cloned[idx].Path = NormalizeHintPath(cloned[idx].Path)
	}
	return cloned
}

func computeCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "image", Title: "Image", Control: "text", Placeholder: "ghcr.io/org/app:latest", Description: "Container image to run for each replica."},
		{Path: "imagePullPolicy", Title: "Image pull policy", Control: "select", Options: []string{"Always", "IfNotPresent", "Never"}, Description: "When the runtime should pull the image before startup."},
		{Path: "replicas", Title: "Replicas", Control: "number", Description: "Desired number of identical workload instances."},
		{Path: "command", Title: "Command", Control: "list", Description: "Override the container entrypoint command."},
		{Path: "args", Title: "Arguments", Control: "list", Description: "Arguments passed to the command or image entrypoint."},
		{Path: "env", Title: "Environment JSON", Control: "json", Description: "Environment variables injected at runtime; values may contain refs or secret refs."},
		{Path: "resources", Title: "Resources JSON", Control: "json", Description: "CPU and memory requests, limits, or reservations."},
		{Path: "healthCheck", Title: "Health check JSON", Control: "json", Description: "Probe or command used to determine workload health."},
		{Path: "privileged", Title: "Privileged", Control: "boolean", Description: "Run the workload with elevated privileges when supported."},
		{Path: "capabilities", Title: "Capabilities", Control: "list", Description: "Extra Linux capabilities granted to the workload."},
		{Path: "networks", Title: "Networks", Control: "asset-refs", RefTypes: []string{"Network"}, Description: "Network assets attached to this workload."},
		{Path: "ports", Title: "Ports JSON", Control: "json", Description: "Listener definitions, optional host or service exposure, and dynamic hostname routing."},
		{Path: "volumeMounts", Title: "Volume mounts JSON", Control: "json", Description: "Volume assets mounted into the workload filesystem."},
		{Path: "configMounts", Title: "Config mounts JSON", Control: "json", Description: "Config assets rendered into files inside the workload."},
		{Path: "hostBindMounts", Title: "Host bind mounts JSON", Control: "json", Description: "Host filesystem paths bound into the workload."},
	}
}

func cdkStackCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "context", Title: "CDK context JSON", Control: "json", Description: "Additional CDK context values merged onto the payload manifest before synth and deploy."},
		{Path: "env", Title: "Environment JSON", Control: "json", Description: "Environment variables exported while running the CDK app; values may contain refs or secret refs."},
	}
}

func sqlDatabaseCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "engine", Title: "Engine", Control: "text", Placeholder: "postgres", Description: "Database engine name used by the backing service."},
		{Path: "version", Title: "Version", Control: "text", Description: "Engine version or image tag to target."},
		{Path: "database", Title: "Database name", Control: "text", Description: "Default database or schema name to create or target."},
		{Path: "user", Title: "User", Control: "text", Description: "Application user to provision or connect as."},
		{Path: "port", Title: "Port", Control: "number", Description: "Listener port exposed by the database service."},
		{Path: "volume", Title: "Volume asset", Control: "asset-ref", RefTypes: []string{"Volume"}, Description: "Volume asset used for database data files."},
		{Path: "config", Title: "Config asset", Control: "asset-ref", RefTypes: []string{"Config"}, Description: "Config asset mounted for bootstrap or tuning files."},
		{Path: "networks", Title: "Networks", Control: "asset-refs", RefTypes: []string{"Network"}, Description: "Network assets attached to the database service."},
	}
}

func volumeCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "size", Title: "Size", Control: "text", Placeholder: "20Gi", Description: "Requested storage capacity for persistent volume backends."},
		{Path: "class", Title: "Storage class", Control: "text", Description: "Storage class or backend driver selection."},
		{Path: "accessMode", Title: "Access mode", Control: "select", Options: []string{"ReadWriteOnce", "ReadWriteMany", "ReadOnlyMany"}, Description: "How many readers or writers may mount the volume."},
		{Path: "ephemeral", Title: "Ephemeral", Control: "boolean", Description: "Create scratch storage removed alongside the workload."},
	}
}

func configCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "format", Title: "Format", Control: "select", Options: []string{"text", "json", "yaml"}, Description: "Serialization format for generated config files."},
		{Path: "content", Title: "Inline content", Control: "textarea", Description: "Single inline config blob rendered as one file."},
		{Path: "data", Title: "Data files JSON", Control: "json", Description: "Multiple named file contents keyed by filename."},
	}
}

func networkCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "driver", Title: "Driver", Control: "select", Options: []string{"bridge", "overlay", "host", "none"}, Description: "Network driver or backend selected by the pusher."},
		{Path: "internal", Title: "Internal only", Control: "boolean", Description: "Restrict the network to internal east-west traffic when supported."},
		{Path: "scope", Title: "Scope", Control: "select", Options: []string{"partition", "cluster"}, Description: "Visibility boundary for the network."},
	}
}

func loadBalancerCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "config", Title: "Config asset", Control: "asset-ref", RefTypes: []string{"Config"}, Description: "Config asset containing proxy, listener, or routing config."},
		{Path: "targets", Title: "Target compute assets", Control: "asset-refs", RefTypes: []string{"Compute"}, Description: "Compute assets that receive traffic from this edge."},
		{Path: "listeners", Title: "Listeners JSON", Control: "json", Description: "Listener definitions. Each entry supports: name, port (backend), protocol, description, externalPort (static pin), dynamic (auto-allocate)."},
		{Path: "networks", Title: "Networks", Control: "asset-refs", RefTypes: []string{"Network"}, Description: "Network assets attached to the load balancer."},
		{Path: "serviceType", Title: "Service type", Control: "select", Options: []string{"LoadBalancer", "NodePort", "ClusterIP"}, Description: "Platform exposure mode used by Kubernetes-like pushers."},
	}
}

func traefikRouteCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "hostname", Title: "Hostname", Control: "text", Placeholder: "doctor.strata", Description: "Developer-facing hostname exposed by the local Traefik stack."},
		{Path: "target", Title: "Target compute asset", Control: "asset-ref", RefTypes: []string{"Compute"}, Description: "Compute asset whose named port should be forwarded."},
		{Path: "portName", Title: "Target port name", Control: "text", Placeholder: "http", Description: "Named compute port to expose. Leave empty only when the target has one port."},
	}
}

func objectStoreCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "engine", Title: "Engine", Control: "text", Placeholder: "minio", Description: "Object storage engine or provider name."},
		{Path: "endpoint", Title: "Existing endpoint", Control: "text", Placeholder: "http://host.docker.internal:19000", Description: "Existing object store endpoint; when set, Guardian treats the asset as external."},
		{Path: "volume", Title: "Volume asset", Control: "asset-ref", RefTypes: []string{"Volume"}, Description: "Volume asset used for self-hosted object store data."},
		{Path: "config", Title: "Config asset", Control: "asset-ref", RefTypes: []string{"Config"}, Description: "Config asset mounted into the object store service."},
		{Path: "buckets", Title: "Buckets", Control: "list", Description: "Bucket names to create or expect."},
		{Path: "networks", Title: "Networks", Control: "asset-refs", RefTypes: []string{"Network"}, Description: "Network assets attached to the object store service."},
		{Path: "region", Title: "Region", Control: "text", Description: "Region or logical location identifier exposed to clients."},
		{Path: "accessKeyID", Title: "Access key ID", Control: "text", Description: "Access key used for external endpoint authentication."},
		{Path: "secretAccessKey", Title: "Secret access key", Control: "text", Description: "Secret key paired with accessKeyID for external authentication."},
		{Path: "usePathStyle", Title: "Path-style addressing", Control: "boolean", Description: "Force path-style S3 addressing when the endpoint requires it."},
		{Path: "versioning", Title: "Versioning", Control: "boolean", Description: "Enable bucket versioning when supported by the backend."},
	}
}

func observabilityCatalogFields() []CatalogField {
	return []CatalogField{
		{Path: "provider", Title: "Provider", Control: "select", Options: []string{"otel"}, Description: "Observability stack or collector provider name."},
		{Path: "endpoint", Title: "Endpoint", Control: "text", Description: "Upstream collector or export endpoint address."},
		{Path: "protocol", Title: "Protocol", Control: "text", Description: "Transport protocol used for the endpoint."},
		{Path: "receivers", Title: "Receivers", Control: "list", Description: "Input protocols or receiver components to enable."},
		{Path: "exporters", Title: "Exporters", Control: "list", Description: "Output sinks or exporter components to enable."},
		{Path: "config", Title: "Config asset", Control: "asset-ref", RefTypes: []string{"Config"}, Description: "Config asset containing collector or agent configuration."},
		{Path: "volume", Title: "Volume asset", Control: "asset-ref", RefTypes: []string{"Volume"}, Description: "Volume asset used for buffering, persistence, or sidecar data."},
		{Path: "networks", Title: "Networks", Control: "asset-refs", RefTypes: []string{"Network"}, Description: "Network assets attached to the observability service."},
	}
}
