// Package api serves Forge's loopback HTTP API and embedded UI.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	"github.com/radryc/guardian/internal/domain/assets"
	"github.com/radryc/guardian/internal/forge/author"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/forge/quota"
	"github.com/radryc/guardian/internal/forge/repoinit"
	"github.com/radryc/guardian/internal/forge/ui"
	"github.com/radryc/guardian/internal/forge/validate"
	"github.com/radryc/guardian/internal/forge/workspace"
	"github.com/radryc/guardian/internal/repoconfig"
	policy "github.com/radryc/monofs/pkg/authz/policy"
	"gopkg.in/yaml.v3"
)

// Options configures the local server.
type Options struct {
	Root        string
	Token       string
	Submit      func(ctx context.Context, req publish.Request) (publish.Result, error)
	DisplayKeys map[string]string
	// ProxyAuthHeader is an identity header set by a trusted loopback reverse
	// proxy (for example X-Forwarded-User). When present on a loopback request,
	// it authenticates the request without the session token.
	ProxyAuthHeader string
	// AllowedOrigins lists browser origins allowed to send mutation requests
	// through the proxy, for example https://forge.example.com.
	AllowedOrigins []string
}

// Server is the loopback API server.
type Server struct {
	root            string
	token           string
	submit          func(ctx context.Context, req publish.Request) (publish.Result, error)
	displayKeys     map[string]string
	proxyAuthHeader string
	allowedOrigins  []string
	handler         http.Handler
}

// New builds a server bound to one repository root.
func New(opts Options) (*Server, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	s := &Server{
		root:            root,
		token:           opts.Token,
		submit:          opts.Submit,
		displayKeys:     opts.DisplayKeys,
		proxyAuthHeader: strings.TrimSpace(opts.ProxyAuthHeader),
		allowedOrigins:  opts.AllowedOrigins,
	}
	mux := http.NewServeMux()
	mux.Handle("/", ui.Handler())
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/partition", s.handlePartition)
	mux.HandleFunc("/api/intent", s.handleIntent)
	mux.HandleFunc("/api/quota", s.handleQuota)
	mux.HandleFunc("/api/access", s.handleAccess)
	mux.HandleFunc("/api/values", s.handleValues)
	mux.HandleFunc("/api/validate", s.handleValidate)
	mux.HandleFunc("/api/asset", s.handleAsset)
	mux.HandleFunc("/api/asset/connect", s.handleConnect)
	mux.HandleFunc("/api/probe", s.handleProbe)
	mux.HandleFunc("/api/secret", s.handleSecret)
	mux.HandleFunc("/api/alert/suggest", s.handleAlertSuggest)
	mux.HandleFunc("/api/alert/write", s.handleAlertWrite)
	mux.HandleFunc("/api/submit", s.handleSubmit)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/display", s.handleDisplay)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	s.handler = s.withAuth(mux)
	return s, nil
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Serve listens on addr and serves until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) (string, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	server := &http.Server{Handler: s.handler}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() { _ = server.Serve(listener) }()
	return listener.Addr().String(), nil
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.authenticated(r) {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("invalid forge token"))
			return
		}
		if r.Method != http.MethodGet {
			origin := r.Header.Get("Origin")
			if origin != "" && !s.originAllowed(origin) {
				writeError(w, http.StatusForbidden, fmt.Errorf("cross-origin request rejected"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.token != "" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Forge-Token")), []byte(s.token)) == 1 {
			return true
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.token)) == 1 {
			return true
		}
	}
	if s.proxyAuthHeader != "" && isLoopbackRemote(r.RemoteAddr) {
		return strings.TrimSpace(r.Header.Get(s.proxyAuthHeader)) != ""
	}
	return false
}

func (s *Server) originAllowed(origin string) bool {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if parsed, err := url.Parse(origin); err == nil {
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			return true
		}
		if strings.EqualFold(parsed.Hostname(), "localhost") {
			return true
		}
	}
	for _, allowed := range s.allowedOrigins {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), origin) {
			return true
		}
	}
	return false
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	refs, err := workspace.Discover(s.root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	result, _ := validate.Run(r.Context(), validate.Options{Root: s.root})
	writeJSON(w, http.StatusOK, map[string]any{
		"root":       s.root,
		"partitions": names,
		"validation": result,
		"display":    s.displayKeys,
	})
}

func (s *Server) handleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"assetTypes":    assets.Catalog(),
		"quotaProfiles": quota.Catalog(s.root),
	})
}

func (s *Server) handlePartition(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.createPartition(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	ref, err := s.findPartition(r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	configPath := filepath.Join(ref.Dir, "config.yaml")
	config, _ := os.ReadFile(configPath)
	detail := map[string]any{
		"name":    ref.Name,
		"dir":     ref.Dir,
		"config":  string(config),
		"intents": []any{},
		"alerts":  []any{},
	}
	intents := []map[string]any{}
	intentDir := filepath.Join(ref.Dir, "intents")
	if entries, err := os.ReadDir(intentDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(intentDir, entry.Name())
			data, _ := os.ReadFile(path)
			var parsed struct {
				Spec struct {
					Values map[string]any `yaml:"values"`
					Assets []struct {
						Type      string         `yaml:"type"`
						Name      string         `yaml:"name"`
						DependsOn []string       `yaml:"dependsOn"`
						Values    map[string]any `yaml:"values"`
					} `yaml:"assets"`
				} `yaml:"spec"`
			}
			_ = yaml.Unmarshal(data, &parsed)
			assets := make([]map[string]any, 0, len(parsed.Spec.Assets))
			for _, asset := range parsed.Spec.Assets {
				assets = append(assets, map[string]any{
					"type": asset.Type, "name": asset.Name, "dependsOn": asset.DependsOn, "values": asset.Values,
				})
			}
			intents = append(intents, map[string]any{
				"name":   strings.TrimSuffix(entry.Name(), ".yaml"),
				"path":   path,
				"yaml":   string(data),
				"values": parsed.Spec.Values,
				"assets": assets,
			})
		}
	}
	alerts := []map[string]any{}
	alertDir := filepath.Join(ref.Dir, "alerts")
	if entries, err := os.ReadDir(alertDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(alertDir, entry.Name())
			data, _ := os.ReadFile(path)
			alerts = append(alerts, map[string]any{"name": entry.Name(), "path": path, "yaml": string(data)})
		}
	}
	detail["intents"] = intents
	detail["alerts"] = alerts
	detail["quotaProfiles"] = quota.Catalog(s.root)

	var manifest struct {
		Spec struct {
			Quota  any            `yaml:"quota"`
			Values map[string]any `yaml:"values"`
		} `yaml:"spec"`
	}
	_ = yaml.Unmarshal(config, &manifest)
	detail["quota"] = manifest.Spec.Quota
	detail["values"] = manifest.Spec.Values

	accessData, accessExists, _ := author.ReadPartitionAccess(ref.Dir)
	detail["accessExists"] = accessExists
	if accessExists {
		detail["access"] = string(accessData)
	} else {
		detail["access"] = author.PartitionAccessTemplate(ref.Name)
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) createPartition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		Repo           string `json:"repo"`
		Source         string `json:"source"`
		Branch         string `json:"branch"`
		DeletionPolicy string `json:"deletionPolicy"`
		Interval       string `json:"interval"`
		Force          bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	configPath, err := author.CreatePartition(author.PartitionOptions{
		Root: s.root, Name: strings.TrimSpace(body.Name),
		DeletionPolicy: body.DeletionPolicy, Interval: body.Interval, Force: body.Force,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repoConfigPath, err := repoinit.EnsureBinding(repoinit.BindingOptions{
		Root: s.root, RepoName: body.Repo, Source: body.Source, Branch: body.Branch,
		Partition: body.Name, PartitionPath: "partitions/" + body.Name,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"name": body.Name, "configPath": configPath, "repoConfigPath": repoConfigPath,
	})
}

type intentBody struct {
	Partition string `json:"partition"`
	Name      string `json:"name"`
	Pusher    string `json:"pusher"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Force     bool   `json:"force"`
}

// handleIntent creates a new intent under a partition. It mirrors
// `forge intent new` so the workbench and CLI author the same manifests.
func (s *Server) handleIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body intentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	path, err := author.CreateIntent(author.IntentOptions{
		PartitionDir: ref.Dir,
		Name:         name,
		Pusher:       strings.TrimSpace(body.Pusher),
		Cluster:      strings.TrimSpace(body.Cluster),
		Namespace:    strings.TrimSpace(body.Namespace),
		Force:        body.Force,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "path": path})
}

type quotaBody struct {
	Partition string        `json:"partition"`
	Quota     *policy.Quota `json:"quota"`
	Clear     bool          `json:"clear"`
}

// handleQuota sets or clears spec.quota on a partition manifest.
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body quotaBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if body.Quota == nil && !body.Clear {
		writeError(w, http.StatusBadRequest, fmt.Errorf("quota object or clear=true is required"))
		return
	}
	if body.Quota != nil {
		if err := quota.Validate(s.root, body.Quota); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	var quota any
	if body.Quota != nil {
		quota = body.Quota
	}
	path, err := author.SetQuota(ref.Dir, quota)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": path, "quota": body.Quota, "cleared": body.Clear || body.Quota == nil,
	})
}

type accessBody struct {
	Partition string `json:"partition"`
	YAML      string `json:"yaml"`
}

// handleAccess writes the partition-scoped access.yaml. The file is validated
// with Guardian's repo-import rules before it is kept.
func (s *Server) handleAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body accessBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if strings.TrimSpace(body.YAML) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("yaml is required"))
		return
	}
	path, err := author.SavePartitionAccess(ref.Dir, ref.Name, s.partitionDisplayPath(), []byte(body.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type valuesBody struct {
	Partition string         `json:"partition"`
	Intent    string         `json:"intent"`
	Asset     string         `json:"asset"`
	Values    map[string]any `json:"values"`
	Clear     bool           `json:"clear"`
}

// handleValues writes a partition, intent, or asset values map.
func (s *Server) handleValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body valuesBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	vals := body.Values
	if body.Clear || len(vals) == 0 {
		vals = nil
	}
	path, err := author.SetValues(ref.Dir, strings.TrimSpace(body.Intent), strings.TrimSpace(body.Asset), vals)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "cleared": vals == nil})
}

// partitionDisplayPath resolves the repository display path used to validate
// monofs scopes in a partition access policy.
func (s *Server) partitionDisplayPath() string {
	cfg, _, _, err := repoconfig.Load(s.root)
	if err != nil || cfg == nil {
		return ""
	}
	return publish.DisplayPathFromURL(cfg.Repo.Source)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	result, _ := validate.Run(r.Context(), validate.Options{Root: s.root, Strict: false})
	writeJSON(w, http.StatusOK, result)
}

type assetBody struct {
	Partition  string         `json:"partition"`
	Intent     string         `json:"intent"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Image      string         `json:"image"`
	Replicas   int            `json:"replicas"`
	Port       int            `json:"port"`
	Properties map[string]any `json:"properties"`
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body assetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	properties := body.Properties
	if properties == nil {
		properties = map[string]any{}
	}
	switch body.Type {
	case "Compute":
		image, _ := properties["image"].(string)
		if image == "" && body.Image != "" {
			image = body.Image
			properties["image"] = image
		}
		if strings.TrimSpace(image) == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("image is required for a Compute asset"))
			return
		}
		if body.Replicas > 0 {
			properties["replicas"] = body.Replicas
		}
		if body.Port > 0 {
			if _, ok := properties["ports"]; !ok {
				properties["ports"] = []map[string]any{{"name": "http", "containerPort": body.Port, "protocol": "TCP"}}
			}
		}
	case "Secret":
		if body.Image != "" {
			properties["secretRef"] = body.Image
		}
	}
	path, err := author.AddAsset(ref.Dir, body.Intent, assetdomain.Spec{
		Type: body.Type, Name: body.Name, Properties: properties,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type connectBody struct {
	Partition string `json:"partition"`
	Intent    string `json:"intent"`
	From      string `json:"from"`
	To        string `json:"to"`
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body connectBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	path, err := author.ConnectAssets(ref.Dir, body.Intent, body.From, body.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type probeBody struct {
	Partition string `json:"partition"`
	Intent    string `json:"intent"`
	Asset     string `json:"asset"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Port      int    `json:"port"`
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body probeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	name := body.Name
	if name == "" {
		name = body.Asset + "-probe"
	}
	properties := map[string]any{"type": "http", "target": body.Asset}
	if body.Path != "" {
		properties["path"] = body.Path
	}
	if body.Port > 0 {
		properties["port"] = body.Port
	}
	path, err := author.AddProbe(ref.Dir, body.Intent, assetdomain.Spec{
		Type: "Probe", Name: name, Properties: properties,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type secretBody struct {
	Partition string `json:"partition"`
	Intent    string `json:"intent"`
	Asset     string `json:"asset"`
	Env       string `json:"env"`
	Ref       string `json:"ref"`
}

func (s *Server) handleSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body secretBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	path, err := author.AddSecretRef(ref.Dir, body.Intent, body.Asset, body.Env, body.Ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type alertBody struct {
	Partition string `json:"partition"`
	Overwrite bool   `json:"overwrite"`
}

func (s *Server) handleAlertSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body alertBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	data, err := author.SuggestAlerts(ref.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"yaml": string(data)})
}

func (s *Server) handleAlertWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var body alertBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	data, err := author.SuggestAlerts(ref.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path := filepath.Join(ref.Dir, "alerts", "suggested-health.yaml")
	if _, err := os.Stat(path); err == nil && !body.Overwrite {
		writeError(w, http.StatusConflict, fmt.Errorf("%s already exists", path))
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

type submitBody struct {
	Partition string `json:"partition"`
	Message   string `json:"message"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	All       bool   `json:"all"`
	DryRun    bool   `json:"dryRun"`
	Confirm   bool   `json:"confirm"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.submit == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("submission is not configured"))
		return
	}
	var body submitBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !body.DryRun && !body.Confirm {
		writeError(w, http.StatusBadRequest, fmt.Errorf("confirm is required to submit a real change"))
		return
	}
	ref, err := s.findPartition(body.Partition)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	result, err := s.submit(r.Context(), publish.Request{
		Root:             s.root,
		PartitionDir:     ref.Dir,
		Partition:        ref.Name,
		Changed:          !body.All,
		CommitMessage:    body.Message,
		PullRequestTitle: body.Title,
		PullRequestBody:  body.Body,
		DryRun:           body.DryRun,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"result": result, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	proposals, err := publish.LoadProposals(s.root)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if proposals == nil {
		proposals = []publish.Proposal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals})
}

func (s *Server) handleDisplay(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"keys": s.displayKeys})
}

func (s *Server) findPartition(name string) (workspace.PartitionRef, error) {
	refs, err := workspace.Discover(s.root)
	if err != nil {
		return workspace.PartitionRef{}, err
	}
	for _, ref := range refs {
		if ref.Name == name {
			return ref, nil
		}
	}
	return workspace.PartitionRef{}, fmt.Errorf("partition %q not found", name)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}
