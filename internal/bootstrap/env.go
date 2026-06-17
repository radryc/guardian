package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComputeEnv builds the complete set of environment variables needed
// for template rendering. Returns an Env map suitable for envsubst.
func ComputeEnv(cfg *Config) (Env, error) {
	env := make(Env)

	// --- Secrets ---
	monofsToken := os.Getenv("MONOFS_TOKEN")
	if monofsToken == "" {
		tok, err := generateToken(32)
		if err != nil {
			return nil, fmt.Errorf("generating monofs token: %w", err)
		}
		monofsToken = tok
	}

	encKey := os.Getenv("MONOFS_ENCRYPTION_KEY")
	if encKey == "" {
		// Try to read from monofs .env file
		encKey = readMonofsEncryptionKey()
	}
	if !isValidEncryptionKey(encKey) {
		return nil, fmt.Errorf("MONOFS_ENCRYPTION_KEY is not configured; run `guardianctl setup` first or set MONOFS_ENCRYPTION_KEY")
	}

	minioAK := os.Getenv("MINIO_ACCESS_KEY")
	if minioAK == "" {
		minioAK = "minioadmin"
	}
	minioSK := os.Getenv("MINIO_SECRET_KEY")
	if minioSK == "" {
		minioSK = "minioadmin"
	}

	clientDiscoveryToken := os.Getenv("CLIENT_DISCOVERY_TOKEN")
	if clientDiscoveryToken == "" {
		tok, err := generateToken(32)
		if err != nil {
			return nil, fmt.Errorf("generating client discovery token: %w", err)
		}
		clientDiscoveryToken = tok
	}

	env["MONOFS_TOKEN_B64"] = b64(monofsToken)
	env["MONOFS_ENCRYPTION_KEY_B64"] = b64(encKey)
	env["MINIO_ACCESS_KEY_B64"] = b64(minioAK)
	env["MINIO_SECRET_KEY_B64"] = b64(minioSK)
	env["CLIENT_DISCOVERY_TOKEN_B64"] = b64(clientDiscoveryToken)

	// --- Namespaces ---
	env["STORAGE_NAMESPACE"] = cfg.Storage.Namespace
	env["LB_NAMESPACE"] = cfg.LB.Namespace
	env["GUARDIAN_NAMESPACE"] = cfg.Guardian.Namespace

	// --- External service ---
	env["EXTERNAL_SERVICE_TYPE"] = cfg.Storage.External.ServiceType
	env["EXTERNAL_SERVICE_SPEC"] = externalServiceSpecYAML()

	// --- Storage images ---
	env["MONOFS_SERVER_IMAGE"] = cfg.Storage.Images.Server
	env["MONOFS_ROUTER_IMAGE"] = cfg.Storage.Images.Router
	env["MONOFS_FETCHER_IMAGE"] = cfg.Storage.Images.Fetcher
	env["MONOFS_SEARCH_IMAGE"] = cfg.Storage.Images.Search
	env["MONOFS_REGISTRY_IMAGE"] = cfg.Storage.Images.Registry
	env["MONOFS_LB_IMAGE"] = cfg.Storage.Images.LB
	env["MINIO_IMAGE"] = cfg.Storage.Images.Minio
	env["MONOFS_IMAGE_PULL_POLICY"] = cfg.Storage.Images.PullPolicy

	// --- PVC sizes ---
	env["MINIO_PVC_SIZE"] = cfg.Storage.PVC.MinioSize
	env["FETCHER_PVC_SIZE"] = cfg.Storage.PVC.FetcherSize
	env["SEARCH_PVC_SIZE"] = cfg.Storage.PVC.SearchSize
	env["NODE_PVC_SIZE"] = cfg.Storage.PVC.NodeSize

	// --- OpenTelemetry ---
	env["MONOFS_OTEL_ENDPOINT"] = cfg.Storage.OTel.Endpoint
	env["MONOFS_OTEL_INSECURE"] = cfg.Storage.OTel.Insecure
	env["MONOFS_OTEL_SERVICE_NAME"] = cfg.Storage.OTel.ServiceName
	env["MONOFS_OTEL_METRIC_INTERVAL"] = cfg.Storage.OTel.MetricInterval

	// --- Cluster ---
	env["MONOFS_CLUSTER_ID"] = cfg.Storage.ClusterID

	// --- Node addresses (internal) ---
	env["MONOFS_NODE_ADDRS"] = internalNodeAddrCSV(cfg)
	env["MONOFS_EXTERNAL_ADDRS"] = defaultExternalAddrCSV(cfg)
	env["SERVER_DIAGNOSTICS_ADDRS"] = serverDiagnosticsAddrsCSV(cfg)

	// --- Diagnostics ---
	env["SEARCH_DIAGNOSTICS_ADDR"] = cfg.Storage.SearchDiagnosticsAddr
	env["FETCHER_DIAGNOSTICS_ADDRS"] = cfg.Storage.FetcherDiagnosticsAddrs

	// --- LB-edge ---
	env["LB_BOOTSTRAP"] = lbNodeBootstrap(cfg)
	env["LB_NODE_PORTS_SPEC"] = lbNodePortsYAML(cfg)
	env["LB_NODE_CONTAINER_PORTS"] = lbNodeContainerPortsYAML(cfg)
	env["LB_USER_SERVICE_PORTS_SPEC"] = lbUserServicePortsYAML(cfg)
	env["LB_USER_SERVICE_CONTAINER_PORTS"] = lbUserServiceContainerPortsYAML(cfg)
	env["LB_KIND_NODE_PLACEMENT"] = lbKindNodePlacementYAML(cfg)
	env["LB_DYNAMIC_PORT_MIN"] = strconv.Itoa(cfg.Storage.External.LBPortMin)
	env["LB_DYNAMIC_PORT_MAX"] = strconv.Itoa(cfg.Storage.External.LBPortMax)

	// --- MonoFS registry ---
	env["MONOFS_REGISTRY_ROUTER_ADDR"] = fmt.Sprintf("monofs-external.%s.svc.cluster.local:9090", cfg.LB.Namespace)
	env["MONOFS_REGISTRY_DEFAULT_UPSTREAM"] = cfg.Storage.Registry.DefaultUpstream
	env["MONOFS_REGISTRY_UPSTREAMS"] = cfg.Storage.Registry.Upstreams

	// --- Guardian ---
	env["GUARDIAN_IMAGE"] = cfg.Guardian.Images.Guardiand
	env["GUARDIAN_IMAGE_PULL_POLICY"] = cfg.Guardian.Images.PullPolicy
	env["GUARDIAN_PUSHER_IMAGE"] = cfg.Guardian.Images.PusherK8s
	env["GUARDIAN_PUSHER_AWS_IMAGE"] = cfg.Guardian.Images.PusherAws
	env["GUARDIAN_LB_IMAGE"] = cfg.Guardian.Images.LB
	env["GUARDIAN_MONOFS_ROUTER"] = cfg.Guardian.Monofs.Router
	env["GUARDIAN_MONOFS_CLIENT_API_ENDPOINT"] = cfg.Guardian.Monofs.Router
	env["GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES"] = cfg.Guardian.Monofs.UseExternalAddresses
	env["GUARDIAN_MONOFS_CLIENT_USE_EXTERNAL_ADDRESSES"] = cfg.Guardian.Monofs.ClientUseExternalAddresses
	env["GUARDIAN_PUSHER_NAME"] = cfg.Guardian.Pushers.K8s.Name
	env["GUARDIAN_CLUSTER"] = cfg.Guardian.Pushers.K8s.Cluster
	env["GUARDIAN_PUSHERS"] = computeGuardianPushers(cfg)
	env["GUARDIAN_UI_PORT"] = cfg.Guardian.UIPort
	env["GUARDIAN_UI_LISTEN"] = cfg.Guardian.UIListen
	env["GUARDIAN_UI_BASE_URL"] = cfg.Guardian.UIBaseURL
	env["GUARDIAN_CLIENT_DISCOVERY_TOKEN"] = clientDiscoveryToken
	env["GUARDIAN_IMAGE_BUILD_REGISTRY"] = cfg.Guardian.ImageBuild.Registry
	env["GUARDIAN_KANIKO_REGISTRY_MIRROR"] = cfg.Guardian.ImageBuild.KanikoMirror
	env["GUARDIAN_KANIKO_DOCKER_CONFIG_SECRET"] = cfg.Guardian.ImageBuild.KanikoDockerConfigSecret
	env["LOCAL_REGISTRY_PORT"] = cfg.Guardian.LocalRegistry.Port
	env["LOCAL_REGISTRY_HOST_ALIASES"] = "" // set during deploy after clusterIP is known

	// --- AWS pusher ---
	env["GUARDIAN_AWS_ACCOUNT"] = cfg.Guardian.Pushers.AWS.Account
	env["GUARDIAN_AWS_REGION"] = cfg.Guardian.Pushers.AWS.Region
	env["GUARDIAN_AWS_PUSHER_NAME"] = awsPusherName(cfg)
	env["GUARDIAN_AWS_ASSUME_ROLE_NAME"] = cfg.Guardian.Pushers.AWS.AssumeRoleName

	return env, nil
}

// ComputeNodeEnv computes the additional env vars for a specific node suffix.
func ComputeNodeEnv(cfg *Config, suffix string) Env {
	env := Env{
		"SUFFIX":            suffix,
		"NODE_NAME":         nodeName(suffix),
		"NODE_EXTERNAL_PORT": nodeExternalPort(cfg, suffix),
		"KVS_EXTRA_ARGS":    kvsExtraArgs(cfg, suffix),
	}
	return env
}

// ComputeRouterEnv computes the additional env vars for a specific router suffix.
func ComputeRouterEnv(cfg *Config, suffix string, externalAddrCSV string) Env {
	if externalAddrCSV == "" {
		externalAddrCSV = defaultExternalAddrCSV(cfg)
	}
	routerNameVal := routerName(suffix)
	return Env{
		"SUFFIX":              suffix,
		"ROUTER_NAME":         routerNameVal,
		"ROUTER_PEER_NAME":    routerPeerName(suffix),
		"MONOFS_CLUSTER_ID":   cfg.Storage.ClusterID,
		"MONOFS_NODE_ADDRS":   internalNodeAddrCSV(cfg),
		"MONOFS_EXTERNAL_ADDRS": externalAddrCSV,
	}
}

// --- Helper functions (mirror Python logic) ---

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func generateToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func readMonofsEncryptionKey() string {
	// Try MONOFS_REPO_DIR/.env then ../monofs/.env
	monofsDir := os.Getenv("MONOFS_REPO_DIR")
	if monofsDir == "" {
		monofsDir = filepath.Join(findRoot(), "..", "monofs")
	}
	data, err := os.ReadFile(filepath.Join(monofsDir, ".env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MONOFS_ENCRYPTION_KEY=") {
			return strings.TrimPrefix(line, "MONOFS_ENCRYPTION_KEY=")
		}
	}
	return ""
}

func isValidEncryptionKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func externalServiceSpecYAML() string {
	ips := configuredExternalServiceIPs()
	if len(ips) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "\n  externalIPs:")
	for _, ip := range ips {
		lines = append(lines, fmt.Sprintf("    - %s", ip))
	}
	return strings.Join(lines, "\n")
}

func configuredExternalServiceIPs() []string {
	raw := os.Getenv("EXTERNAL_SERVICE_IPS")
	if raw == "" {
		raw = os.Getenv("EXTERNAL_SERVICE_IP")
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func internalNodeAddrCSV(cfg *Config) string {
	var parts []string
	for _, name := range cfg.Storage.NodeNames {
		parts = append(parts, fmt.Sprintf("%s=%s.%s.svc.cluster.local:9000", name, name, cfg.Storage.Namespace))
	}
	return strings.Join(parts, ",")
}

func defaultExternalAddrCSV(cfg *Config) string {
	var parts []string
	for _, name := range cfg.Storage.NodeNames {
		port := nodeExternalPort(cfg, name)
		addr := defaultExternalAddr(cfg, name, port)
		parts = append(parts, fmt.Sprintf("%s=%s", name, addr))
	}
	return strings.Join(parts, ",")
}

func defaultExternalAddr(cfg *Config, name, port string) string {
	if cfg.Storage.External.AddressTemplate != "" {
		addr := cfg.Storage.External.AddressTemplate
		addr = strings.ReplaceAll(addr, "{name}", name)
		addr = strings.ReplaceAll(addr, "{namespace}", cfg.Storage.Namespace)
		addr = strings.ReplaceAll(addr, "{port}", port)
		return addr
	}
	return fmt.Sprintf("%s-external.%s.svc.cluster.local:%s", name, cfg.Storage.Namespace, port)
}

func serverDiagnosticsAddrsCSV(cfg *Config) string {
	var parts []string
	for _, name := range cfg.Storage.NodeNames {
		parts = append(parts, fmt.Sprintf("%s=%s:9100", name, name))
	}
	return strings.Join(parts, ",")
}

func nodeName(suffix string) string {
	if strings.HasPrefix(suffix, "node-") {
		return suffix
	}
	return "node-" + suffix
}

func nodeExternalPort(cfg *Config, name string) string {
	n := nodeName(name)
	if port, ok := cfg.Storage.NodeExternalPorts[n]; ok {
		return port
	}
	return "9000"
}

// lbNodeBootstrap builds the LB_BOOTSTRAP entries for node external port -> internal ClusterIP routing.
func lbNodeBootstrap(cfg *Config) string {
	var entries []string
	for _, name := range cfg.Storage.NodeNames {
		port := nodeExternalPort(cfg, name)
		internal := fmt.Sprintf("%s.%s.svc.cluster.local:9000", name, cfg.Storage.Namespace)
		entry := fmt.Sprintf("%s@grpc%s:%s=%s", name, "[MonoFS "+name+" gRPC]", port, internal)
		entries = append(entries, entry)
	}
	// Also add the static service entries for monofs HTTP/GRPC + guardian UI
	monofsGrpc := fmt.Sprintf("monofs-grpc@grpc[MonoFS gRPC API]:9090=router-a.%s.svc.cluster.local:9090,router-b.%s.svc.cluster.local:9090",
		cfg.Storage.Namespace, cfg.Storage.Namespace)
	monofsHTTP := fmt.Sprintf("monofs-http@http[MonoFS HTTP UI]:8080=router-a.%s.svc.cluster.local:8080,router-b.%s.svc.cluster.local:8080",
		cfg.Storage.Namespace, cfg.Storage.Namespace)
	guardianUI := fmt.Sprintf("guardian-ui@http[Guardian UI]:8090=guardian-ui.%s.svc.cluster.local:%s",
		cfg.Guardian.Namespace, cfg.Guardian.UIPort)
	registry := fmt.Sprintf("registry@http[Registry]:5000=%s.%s.svc.cluster.local:5000",
		cfg.Guardian.LocalRegistry.Name, cfg.Guardian.LocalRegistry.Namespace)

	return monofsGrpc + ";" + monofsHTTP + ";" + guardianUI + ";" + registry + ";" + strings.Join(entries, ";")
}

func lbNodePortsYAML(cfg *Config) string {
	var lines []string
	for _, name := range cfg.Storage.NodeNames {
		port := nodeExternalPort(cfg, name)
		lines = append(lines, fmt.Sprintf("    - name: %s-grpc\n      port: %s\n      targetPort: %s", name, port, port))
	}
	return strings.Join(lines, "\n")
}

func lbNodeContainerPortsYAML(cfg *Config) string {
	var lines []string
	for _, name := range cfg.Storage.NodeNames {
		port := nodeExternalPort(cfg, name)
		lines = append(lines, fmt.Sprintf("            - containerPort: %s", port))
	}
	return strings.Join(lines, "\n")
}

func lbUserServicePortsYAML(cfg *Config) string {
	var lines []string
	for _, port := range cfg.Storage.UserServicePorts {
		lines = append(lines, fmt.Sprintf("    - name: user-svc-%d\n      port: %d\n      targetPort: %d", port, port, port))
	}
	return strings.Join(lines, "\n")
}

func lbUserServiceContainerPortsYAML(cfg *Config) string {
	var lines []string
	for _, port := range cfg.Storage.UserServicePorts {
		lines = append(lines, fmt.Sprintf("            - containerPort: %d", port))
	}
	return strings.Join(lines, "\n")
}

func lbKindNodePlacementYAML(cfg *Config) string {
	if !cfg.LB.PinToControlPlane {
		return ""
	}
	return `
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      tolerations:
        - key: node-role.kubernetes.io/control-plane
          operator: Exists
          effect: NoSchedule`
}

func kvsExtraArgs(cfg *Config, suffix string) string {
	n := nodeName(suffix)
	allNodes := cfg.Storage.NodeNames
	var lines []string
	if n == "node-a" {
		lines = append(lines, "            - --kvs-bootstrap")
	}
	for _, peer := range allNodes {
		if peer != n {
			lines = append(lines, fmt.Sprintf("            - --kvs-peer=%s,%s:9000,%s:7000", peer, peer, peer))
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n") + "\n"
	}
	return ""
}

func routerName(suffix string) string {
	return "router-" + suffix
}

func routerPeerName(suffix string) string {
	if suffix == "a" {
		return routerName("b")
	}
	return routerName("a")
}

func awsPusherName(cfg *Config) string {
	if cfg.Guardian.Pushers.AWS.Account != "" {
		return "aws-" + cfg.Guardian.Pushers.AWS.Account
	}
	return ""
}

func computeGuardianPushers(cfg *Config) string {
	parts := []string{
		fmt.Sprintf("%s:/.queues/%s", cfg.Guardian.Pushers.K8s.Name, cfg.Guardian.Pushers.K8s.Name),
		fmt.Sprintf("%s:/.queues/%s", cfg.Guardian.Pushers.Docker.Name, cfg.Guardian.Pushers.Docker.Name),
	}
	if cfg.Guardian.Pushers.AWS.Enabled && cfg.Guardian.Pushers.AWS.Account != "" {
		name := awsPusherName(cfg)
		parts = append(parts, fmt.Sprintf("%s:/.queues/%s", name, name))
	}
	return strings.Join(parts, ",")
}

// --- kubectl helpers ---

func kubectlQuery(args ...string) string {
	cmd := exec.Command("kubectl", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ServiceExternalEndpoint resolves the externally reachable endpoint for a service.
func ServiceExternalEndpoint(namespace, serviceName, portName, servicePort string) string {
	// Check externalIPs first
	host := kubectlQuery("-n", namespace, "get", "service", serviceName, "-o", "jsonpath={.spec.externalIPs[0]}")
	if host != "" {
		return fmt.Sprintf("%s:%s", host, servicePort)
	}
	host = kubectlQuery("-n", namespace, "get", "service", serviceName, "-o", "jsonpath={.spec.loadBalancerIP}")
	if host != "" {
		return fmt.Sprintf("%s:%s", host, servicePort)
	}

	svcType := kubectlQuery("-n", namespace, "get", "service", serviceName, "-o", "jsonpath={.spec.type}")
	if svcType == "LoadBalancer" {
		host = kubectlQuery("-n", namespace, "get", "service", serviceName, "-o", "jsonpath={.status.loadBalancer.ingress[0].hostname}")
		if host == "" {
			host = kubectlQuery("-n", namespace, "get", "service", serviceName, "-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
		}
		if isDockerInternalIP(host) {
			host = "127.0.0.1"
		}
		if host != "" {
			return fmt.Sprintf("%s:%s", host, servicePort)
		}
		// Fall back to NodePort
		nodeHost := firstNodeAddress()
		nodePort := kubectlQuery("-n", namespace, "get", "service", serviceName, fmt.Sprintf("jsonpath={.spec.ports[?(@.name=='%s')].nodePort}", portName))
		if nodeHost != "" && nodePort != "" {
			return fmt.Sprintf("%s:%s", nodeHost, nodePort)
		}
		return ""
	}
	if svcType == "NodePort" {
		nodeHost := firstNodeAddress()
		nodePort := kubectlQuery("-n", namespace, "get", "service", serviceName, fmt.Sprintf("jsonpath={.spec.ports[?(@.name=='%s')].nodePort}", portName))
		if nodeHost != "" && nodePort != "" {
			return fmt.Sprintf("%s:%s", nodeHost, nodePort)
		}
		return ""
	}
	return ""
}

func isDockerInternalIP(host string) bool {
	if strings.HasPrefix(host, "192.168.") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		parts := strings.Split(host, ".")
		if len(parts) == 4 {
			second, _ := strconv.Atoi(parts[1])
			return second >= 16 && second <= 31
		}
	}
	return false
}

func firstNodeAddress() string {
	addr := kubectlQuery("get", "nodes", "-o", "jsonpath={.items[0].status.addresses[?(@.type=='ExternalIP')].address}")
	if addr != "" {
		return addr
	}
	return kubectlQuery("get", "nodes", "-o", "jsonpath={.items[0].status.addresses[?(@.type=='InternalIP')].address}")
}

// LbEdgeEndpoint returns the external endpoint for the lb-edge service.
func LbEdgeEndpoint(namespace, portName, servicePort string) string {
	ips := configuredExternalServiceIPs()
	if len(ips) > 0 {
		return fmt.Sprintf("%s:%s", ips[0], servicePort)
	}
	return ServiceExternalEndpoint(namespace, "monofs-external", portName, servicePort)
}

// PartitionIntentPath helper for stamping URLs into partition YAMLs.
type PartitionIntentPath struct {
	Path string
}

// SetEnvInIntent sets an env var value in a YAML intent file's env list.
func SetEnvInIntent(path string, envName, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var docs []map[string]interface{}
	single := false
	// Try single document first — yaml.v3 will decode a bare mapping into a slice,
	// so we check for the list indicator instead.
	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	if strings.HasPrefix(trimmed, "-") {
		if err := yaml.Unmarshal(data, &docs); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	} else {
		var doc map[string]interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		docs = []map[string]interface{}{doc}
		single = true
	}

	changed := false
	for _, doc := range docs {
		if walkAndSetEnv(doc, envName, value) {
			changed = true
		}
		if walkAndSetDictEnv(doc, envName, value) {
			changed = true
		}
	}

	if !changed {
		return nil
	}

	var out []byte
	if single {
		out, err = yaml.Marshal(docs[0])
	} else {
		out, err = yaml.Marshal(docs)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func walkAndSetEnv(node interface{}, envName, value string) bool {
	m, ok := node.(map[string]interface{})
	if !ok {
		return false
	}
	changed := false
	for k, v := range m {
		if k == "env" {
			if envList, ok := v.([]interface{}); ok {
				for _, e := range envList {
					if em, ok := e.(map[string]interface{}); ok {
						if em["name"] == envName {
							em["value"] = value
							changed = true
						}
					}
				}
			}
		}
		if child, ok := v.(map[string]interface{}); ok {
			if walkAndSetEnv(child, envName, value) {
				changed = true
			}
		}
		if childList, ok := v.([]interface{}); ok {
			for _, item := range childList {
				if walkAndSetEnv(item, envName, value) {
					changed = true
				}
			}
		}
	}
	return changed
}

func walkAndSetDictEnv(node interface{}, envName, value string) bool {
	m, ok := node.(map[string]interface{})
	if !ok {
		return false
	}
	changed := false
	for k, v := range m {
		if k == "env" {
			if envMap, ok := v.(map[string]interface{}); ok {
				if _, exists := envMap[envName]; exists {
					envMap[envName] = value
					changed = true
				}
			}
		}
		if child, ok := v.(map[string]interface{}); ok {
			if walkAndSetDictEnv(child, envName, value) {
				changed = true
			}
		}
		if childList, ok := v.([]interface{}); ok {
			for _, item := range childList {
				if walkAndSetDictEnv(item, envName, value) {
					changed = true
				}
			}
		}
	}
	return changed
}

// SetTopLevelKey sets a top-level key in a YAML config file.
func SetTopLevelKey(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	if doc == nil {
		doc = make(map[string]interface{})
	}
	doc[key] = value

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// GenerateEncryptionKey generates a new 64-char hex encryption key.
func GenerateEncryptionKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// WriteMonofsEnv writes an encryption key to ../monofs/.env.
func WriteMonofsEnv(key string) error {
	monofsDir := os.Getenv("MONOFS_REPO_DIR")
	if monofsDir == "" {
		monofsDir = filepath.Join(findRoot(), "..", "monofs")
	}
	if err := os.MkdirAll(monofsDir, 0755); err != nil {
		return err
	}
	content := fmt.Sprintf("MONOFS_ENCRYPTION_KEY=%s\n", key)
	return os.WriteFile(filepath.Join(monofsDir, ".env"), []byte(content), 0644)
}

// KubectlGetServiceIP returns the cluster IP of a service.
func KubectlGetServiceIP(namespace, name string) string {
	return kubectlQuery("-n", namespace, "get", "svc", name, "-o", "jsonpath={.spec.clusterIP}")
}

// KubectlGetSecretData decodes a base64-encoded secret data key.
func KubectlGetSecretData(namespace, secretName, key string) string {
	encoded := kubectlQuery("-n", namespace, "get", "secret", secretName, fmt.Sprintf("-o=jsonpath={.data.%s}", key))
	if encoded == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func ComputeLocalRegistryHostAliases(cfg *Config) string {
	clusterIP := KubectlGetServiceIP(cfg.Guardian.LocalRegistry.Namespace, cfg.Guardian.LocalRegistry.Name)
	if clusterIP == "" {
		return ""
	}
	host := strings.SplitN(cfg.Guardian.LocalRegistry.Host, ":", 2)[0]
	return fmt.Sprintf("      hostAliases:\n        - ip: %q\n          hostnames:\n            - %q\n", clusterIP, host)
}

// LbEdgeRegisteredPorts queries the lb-edge registry for registered external ports.
func LbEdgeRegisteredPorts(adminPort int) []int {
	url := fmt.Sprintf("http://127.0.0.1:%d/services", adminPort)
	_ = url
	return nil
}
