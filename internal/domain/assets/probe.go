package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

// ProbeSpec describes an explicit health probe. When Target is set, the probe
// overrides autoprobe for that asset; otherwise it is an independent probe.
type ProbeSpec struct {
	Target             string            `json:"target,omitempty" yaml:"target,omitempty"`
	Type               string            `json:"type,omitempty" yaml:"type,omitempty"`
	Name               string            `json:"name,omitempty" yaml:"name,omitempty"`
	URL                string            `json:"url,omitempty" yaml:"url,omitempty"`
	Address            string            `json:"address,omitempty" yaml:"address,omitempty"`
	Port               int               `json:"port,omitempty" yaml:"port,omitempty"`
	Path               string            `json:"path,omitempty" yaml:"path,omitempty"`
	Method             string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers            map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	ExpectStatus       []int             `json:"expectStatus,omitempty" yaml:"expectStatus,omitempty"`
	ExpectBody         string            `json:"expectBody,omitempty" yaml:"expectBody,omitempty"`
	ExpectIPs          []string          `json:"expectIps,omitempty" yaml:"expectIps,omitempty"`
	Interval           string            `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout            string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries            *int              `json:"retries,omitempty" yaml:"retries,omitempty"`
	InsecureSkipVerify *bool             `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
	Enabled            *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Labels             map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type probeDefinition struct{}

func init() {
	Register(probeDefinition{})
}

func (probeDefinition) Type() string { return assetdomain.TypeProbe }

func (probeDefinition) NewSpec() any { return &ProbeSpec{} }

func (probeDefinition) Validate(spec any, ctx ValidationContext) error {
	typed, ok := spec.(*ProbeSpec)
	if !ok {
		return fmt.Errorf("internal probe spec type mismatch")
	}
	switch strings.ToLower(strings.TrimSpace(typed.Type)) {
	case "", "http", "https", "tcp", "grpc", "dns", "tls":
	default:
		return fmt.Errorf("property type has unsupported value %q", typed.Type)
	}
	if typed.Target != "" {
		if _, ok := ctx.AssetTypes[typed.Target]; !ok {
			return fmt.Errorf("target %q is not defined in this intent", typed.Target)
		}
	}
	if typed.Target == "" && typed.URL == "" && typed.Address == "" && typed.Name == "" {
		return fmt.Errorf("requires target, url, address, or name")
	}
	for idx, status := range typed.ExpectStatus {
		if status < 100 || status > 599 {
			return fmt.Errorf("expectStatus[%d] must be a valid HTTP status", idx)
		}
	}
	if typed.Port < 0 || typed.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
