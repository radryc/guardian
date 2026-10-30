package assets

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

var (
	probeVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	probeStepName     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	probeGRPCName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	probeMethodName   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	probeHTTPMethod   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]*$`)
)

// ProbeSpec describes an explicit health probe. When Target is set, the probe
// overrides autoprobe for that asset; otherwise it is an independent probe.
//
// A probe without Steps is a single request/connection check. When Steps is
// set, the probe becomes a synthetic user journey: steps run in order and
// later steps can interpolate values captured from earlier responses.
type ProbeSpec struct {
	Target             string            `json:"target,omitempty" yaml:"target,omitempty"`
	Type               string            `json:"type,omitempty" yaml:"type,omitempty"`
	Name               string            `json:"name,omitempty" yaml:"name,omitempty"`
	URL                string            `json:"url,omitempty" yaml:"url,omitempty"`
	Address            string            `json:"address,omitempty" yaml:"address,omitempty"`
	Port               int               `json:"port,omitempty" yaml:"port,omitempty"`
	Path               string            `json:"path,omitempty" yaml:"path,omitempty"`
	Method             string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers            map[string]any    `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body               any               `json:"body,omitempty" yaml:"body,omitempty"`
	ExpectStatus       []int             `json:"expectStatus,omitempty" yaml:"expectStatus,omitempty"`
	ExpectBody         string            `json:"expectBody,omitempty" yaml:"expectBody,omitempty"`
	ExpectIPs          []string          `json:"expectIps,omitempty" yaml:"expectIps,omitempty"`
	Interval           string            `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout            string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries            *int              `json:"retries,omitempty" yaml:"retries,omitempty"`
	InsecureSkipVerify *bool             `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
	Enabled            *bool             `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Labels             map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Steps              []ProbeStep       `json:"steps,omitempty" yaml:"steps,omitempty"`
}

// ProbeStep is one request in a synthetic journey. HTTP steps use URL/Path,
// Method, Headers and Body. gRPC steps use Service, GRPCMethod, Metadata and
// Request. Both support capture and patch.
type ProbeStep struct {
	Name               string            `json:"name,omitempty" yaml:"name,omitempty"`
	Type               string            `json:"type,omitempty" yaml:"type,omitempty"`
	URL                string            `json:"url,omitempty" yaml:"url,omitempty"`
	Address            string            `json:"address,omitempty" yaml:"address,omitempty"`
	Port               int               `json:"port,omitempty" yaml:"port,omitempty"`
	Path               string            `json:"path,omitempty" yaml:"path,omitempty"`
	Method             string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers            map[string]any    `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body               any               `json:"body,omitempty" yaml:"body,omitempty"`
	Patch              map[string]any    `json:"patch,omitempty" yaml:"patch,omitempty"`
	Service            string            `json:"service,omitempty" yaml:"service,omitempty"`
	GRPCMethod         string            `json:"grpcMethod,omitempty" yaml:"grpcMethod,omitempty"`
	Request            any               `json:"request,omitempty" yaml:"request,omitempty"`
	Metadata           map[string]any    `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	ExpectStatus       []int             `json:"expectStatus,omitempty" yaml:"expectStatus,omitempty"`
	ExpectBody         string            `json:"expectBody,omitempty" yaml:"expectBody,omitempty"`
	ExpectJSON         map[string]any    `json:"expectJson,omitempty" yaml:"expectJson,omitempty"`
	Capture            map[string]string `json:"capture,omitempty" yaml:"capture,omitempty"`
	Timeout            string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	InsecureSkipVerify *bool             `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
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
	if len(typed.Steps) == 0 {
		if typed.Target == "" && typed.URL == "" && typed.Address == "" {
			if !strings.EqualFold(strings.TrimSpace(typed.Type), "dns") || strings.TrimSpace(typed.Name) == "" {
				return fmt.Errorf("requires target, url, address, or a dns name")
			}
		}
		for idx, status := range typed.ExpectStatus {
			if status < 100 || status > 599 {
				return fmt.Errorf("expectStatus[%d] must be a valid HTTP status", idx)
			}
		}
	} else if err := validateProbeSteps(typed); err != nil {
		return err
	}
	if typed.Port < 0 || typed.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if value := strings.TrimSpace(typed.Interval); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return fmt.Errorf("interval must be a positive duration")
		}
	}
	if value := strings.TrimSpace(typed.Timeout); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return fmt.Errorf("timeout must be a positive duration")
		}
	}
	if typed.Retries != nil && *typed.Retries < 0 {
		return fmt.Errorf("retries must be zero or greater")
	}
	return nil
}

func validateProbeSteps(spec *ProbeSpec) error {
	if len(spec.Steps) > 32 {
		return fmt.Errorf("steps must contain at most 32 entries")
	}
	topType := strings.ToLower(strings.TrimSpace(spec.Type))
	switch topType {
	case "", "http", "https", "grpc":
	default:
		return fmt.Errorf("steps are only supported for http, https, or grpc probes")
	}

	baseTarget := strings.TrimSpace(spec.Target) != "" ||
		strings.TrimSpace(spec.URL) != "" ||
		strings.TrimSpace(spec.Address) != ""
	hasStepTarget := false
	names := map[string]struct{}{}
	for idx, step := range spec.Steps {
		label := fmt.Sprintf("steps[%d]", idx)
		if name := strings.TrimSpace(step.Name); name != "" {
			if !probeStepName.MatchString(name) {
				return fmt.Errorf("%s: name %q must start with a letter and contain only letters, digits, dashes, or underscores", label, name)
			}
			if _, exists := names[name]; exists {
				return fmt.Errorf("%s: duplicate step name %q", label, name)
			}
			names[name] = struct{}{}
			label = fmt.Sprintf("step %q", name)
		}

		probeType := strings.ToLower(strings.TrimSpace(step.Type))
		if probeType == "" {
			if strings.TrimSpace(step.Service) != "" || strings.TrimSpace(step.GRPCMethod) != "" {
				probeType = "grpc"
			} else if topType != "" {
				probeType = topType
			} else {
				probeType = "http"
			}
		}
		switch probeType {
		case "http", "https":
			method := strings.TrimSpace(step.Method)
			if method == "" {
				method = "GET"
			}
			if !probeHTTPMethod.MatchString(method) {
				return fmt.Errorf("%s: invalid HTTP method %q", label, step.Method)
			}
			for statusIdx, status := range step.ExpectStatus {
				if status < 100 || status > 599 {
					return fmt.Errorf("%s: expectStatus[%d] must be a valid HTTP status", label, statusIdx)
				}
			}
		case "grpc":
			service := strings.TrimSpace(step.Service)
			method := strings.TrimSpace(step.GRPCMethod)
			if method != "" && service == "" {
				return fmt.Errorf("%s: grpc service is required when grpcMethod is set", label)
			}
			if service != "" && !probeGRPCName.MatchString(service) {
				return fmt.Errorf("%s: invalid grpc service %q", label, service)
			}
			if method != "" && !probeMethodName.MatchString(method) {
				return fmt.Errorf("%s: invalid grpc method %q", label, method)
			}
			for statusIdx, status := range step.ExpectStatus {
				if status < 0 || status > 16 {
					return fmt.Errorf("%s: expectStatus[%d] must be a gRPC status code (0-16)", label, statusIdx)
				}
			}
		default:
			return fmt.Errorf("%s: unsupported type %q (http, https, grpc)", label, step.Type)
		}

		if step.Port < 0 || step.Port > 65535 {
			return fmt.Errorf("%s: port must be between 1 and 65535", label)
		}
		if strings.TrimSpace(step.URL) != "" || strings.TrimSpace(step.Address) != "" {
			hasStepTarget = true
		}
		if value := strings.TrimSpace(step.Timeout); value != "" {
			timeout, err := time.ParseDuration(value)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("%s: timeout must be a positive duration", label)
			}
		}
		for key, expression := range step.Capture {
			if !probeVariableName.MatchString(key) {
				return fmt.Errorf("%s: capture name %q must start with a letter or underscore and contain only letters, digits, or underscores", label, key)
			}
			if strings.TrimSpace(expression) == "" {
				return fmt.Errorf("%s: capture %q must have an expression", label, key)
			}
		}
		for path := range step.ExpectJSON {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("%s: expectJson path must not be empty", label)
			}
		}
	}
	if !baseTarget && !hasStepTarget {
		return fmt.Errorf("steps require a target, url, address, or a url/address on each step")
	}
	return nil
}
