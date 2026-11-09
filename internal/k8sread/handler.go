// Package k8sread serves the Kubernetes pusher's observed resource inventory.
package k8sread

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/k8sobserve"
)

// Handler serves the read-only Kubernetes resource API.
type Handler struct {
	Service   *k8sobserve.Service
	Pusher    string
	Cluster   string
	Token     string
	TokenFile string
}

// NewHandler builds a handler for the service.
func NewHandler(service *k8sobserve.Service, pusher, cluster, token string) *Handler {
	return &Handler{Service: service, Pusher: pusher, Cluster: cluster, Token: strings.TrimSpace(token)}
}

// WithTokenFile re-reads the bearer token from disk on each request.
func (h *Handler) WithTokenFile(path string) *Handler {
	h.TokenFile = strings.TrimSpace(path)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.tokenValid(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case path == "/v1/k8s/meta":
		writeJSON(w, http.StatusOK, map[string]string{
			"provider": "kubernetes",
			"pusher":   h.Pusher,
			"cluster":  h.Cluster,
		})
	case path == "/v1/k8s/observed/stacks":
		writeJSON(w, http.StatusOK, map[string]any{"stacks": h.filteredStacks(r)})
	case strings.HasPrefix(path, "/v1/k8s/observed/stacks/"):
		h.handleStack(w, r, strings.TrimPrefix(path, "/v1/k8s/observed/stacks/"))
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) filteredStacks(r *http.Request) []k8sobserve.Inventory {
	stacks := h.Service.List()
	partition := strings.TrimSpace(r.URL.Query().Get("partition"))
	intent := strings.TrimSpace(r.URL.Query().Get("intent"))
	if partition == "" && intent == "" {
		return stacks
	}
	out := make([]k8sobserve.Inventory, 0, len(stacks))
	for _, stack := range stacks {
		if partition != "" && !strings.EqualFold(stack.Partition, partition) {
			continue
		}
		if intent != "" && !strings.EqualFold(stack.Intent, intent) {
			continue
		}
		out = append(out, stack)
	}
	return out
}

func (h *Handler) handleStack(w http.ResponseWriter, r *http.Request, suffix string) {
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		writeError(w, http.StatusBadRequest, "stack name is required")
		return
	}
	if strings.HasSuffix(suffix, "/resources") {
		name := strings.TrimSuffix(suffix, "/resources")
		stack, ok := h.Service.Get(name)
		if !ok {
			writeError(w, http.StatusNotFound, "stack not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stackName": name, "resources": stack.Resources})
		return
	}
	stack, ok := h.Service.Get(suffix)
	if !ok {
		writeError(w, http.StatusNotFound, "stack not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stack": stack})
}

func (h *Handler) tokenValid(r *http.Request) bool {
	token := h.Token
	if h.TokenFile != "" {
		content, err := os.ReadFile(h.TokenFile)
		if err != nil {
			// A configured token file that cannot be read must never fall back
			// to an empty token (which would disable authentication).
			return false
		}
		token = strings.TrimSpace(string(content))
		if token == "" {
			return false
		}
	}
	if token == "" {
		return true
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return false
	}
	provided := strings.TrimSpace(header[len("bearer "):])
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
