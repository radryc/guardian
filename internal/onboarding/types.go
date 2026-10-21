// Package onboarding defines the contract a customer CDK stack publishes for
// Guardian observed onboarding. The descriptor is stored in SSM Parameter
// Store by the GuardianObserve construct and read by the AWS pusher. Guardian
// pulls it from the pusher's read API; the pusher never dials MonoFS or
// Guardian.
package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const (
	APIVersion = "guardian/v1alpha1"
	Kind       = "Onboarding"
)

// Descriptor tells Guardian which partition and intent a deployed stack
// belongs to, where its source repository lives, and which probes and
// telemetry settings apply.
type Descriptor struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`

	Account string `json:"account" yaml:"account"`
	Region  string `json:"region" yaml:"region"`
	Stack   string `json:"stack" yaml:"stack"`

	// Partition is one per account environment. Multiple stacks may share it.
	Partition string `json:"partition" yaml:"partition"`
	// Intent defaults to a slug of Stack.
	Intent string `json:"intent" yaml:"intent"`

	Repo      *Repo      `json:"repo,omitempty" yaml:"repo,omitempty"`
	Publish   *Publish   `json:"publish,omitempty" yaml:"publish,omitempty"`
	Probes    []Probe    `json:"probes,omitempty" yaml:"probes,omitempty"`
	Telemetry *Telemetry `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
}

// Repo describes read-only source access. Guardian clones and synthesizes the
// app in its own control plane; the customer repository is never mutated.
type Repo struct {
	URL        string `json:"url" yaml:"url"`
	Branch     string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Selector   string `json:"selector,omitempty" yaml:"selector,omitempty"`
	SourcePath string `json:"sourcePath,omitempty" yaml:"sourcePath,omitempty"`
}

// Publish selects what Guardian does with the generated partition bundle.
type Publish struct {
	// Mode is "store" (MonoFS only) or "repo" (Guardian-owned repository).
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
	// Repo is a Guardian-owned repository name or URL. Ignored for "store".
	Repo   string `json:"repo,omitempty" yaml:"repo,omitempty"`
	Branch string `json:"branch,omitempty" yaml:"branch,omitempty"`
}

// Probe is an HTTP/TCP/TLS/DNS target owned by the observed stack.
type Probe struct {
	Name         string            `json:"name" yaml:"name"`
	Type         string            `json:"type,omitempty" yaml:"type,omitempty"`
	URL          string            `json:"url,omitempty" yaml:"url,omitempty"`
	Address      string            `json:"address,omitempty" yaml:"address,omitempty"`
	Port         int               `json:"port,omitempty" yaml:"port,omitempty"`
	Path         string            `json:"path,omitempty" yaml:"path,omitempty"`
	Method       string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers      map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	ExpectStatus []int             `json:"expectStatus,omitempty" yaml:"expectStatus,omitempty"`
	Interval     string            `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout      string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries      *int              `json:"retries,omitempty" yaml:"retries,omitempty"`
	Insecure     *bool             `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// Telemetry describes the customer-side ingestion path. It is metadata for
// dashboards and diagnostics; the pusher only queries CloudWatch/X-Ray.
type Telemetry struct {
	Mode     string `json:"mode,omitempty" yaml:"mode,omitempty"`
	LogGroup string `json:"logGroup,omitempty" yaml:"logGroup,omitempty"`
	// Source is "aws" for CloudWatch-backed dashboards or "doctor" when the
	// demo sends probes through the central Prometheus pipeline.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
}

// Reader lists onboarding descriptors from SSM Parameter Store.
type Reader interface {
	List(ctx context.Context) ([]Descriptor, error)
}

// SSMReader reads JSON descriptors under a parameter prefix.
type SSMReader struct {
	Client  SSMAPI
	Prefix  string
	Account string
	Region  string
}

// SSMAPI is the subset of the SSM client used by SSMReader.
type SSMAPI interface {
	GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

func (r *SSMReader) List(ctx context.Context) ([]Descriptor, error) {
	if r == nil || r.Client == nil {
		return nil, nil
	}
	prefix := strings.TrimRight(strings.TrimSpace(r.Prefix), "/")
	if prefix == "" {
		return nil, nil
	}
	out := make([]Descriptor, 0)
	var nextToken *string
	for {
		page, err := r.Client.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path:           aws.String(prefix),
			Recursive:      aws.Bool(true),
			WithDecryption: aws.Bool(true),
			NextToken:      nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list onboarding descriptors under %s: %w", prefix, err)
		}
		for _, parameter := range page.Parameters {
			value := aws.ToString(parameter.Value)
			if strings.TrimSpace(value) == "" {
				continue
			}
			var descriptor Descriptor
			if err := json.Unmarshal([]byte(value), &descriptor); err != nil {
				return nil, fmt.Errorf("parse onboarding descriptor %s: %w", aws.ToString(parameter.Name), err)
			}
			descriptor.applyDefaults(r.Account, r.Region)
			if descriptor.Stack == "" || descriptor.Partition == "" {
				continue
			}
			out = append(out, descriptor)
		}
		if page.NextToken == nil || aws.ToString(page.NextToken) == "" {
			break
		}
		nextToken = page.NextToken
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Partition != out[j].Partition {
			return out[i].Partition < out[j].Partition
		}
		return out[i].Stack < out[j].Stack
	})
	return out, nil
}

func (d *Descriptor) applyDefaults(account, region string) {
	if strings.TrimSpace(d.APIVersion) == "" {
		d.APIVersion = APIVersion
	}
	if strings.TrimSpace(d.Kind) == "" {
		d.Kind = Kind
	}
	if strings.TrimSpace(d.Account) == "" {
		d.Account = strings.TrimSpace(account)
	}
	if strings.TrimSpace(d.Region) == "" {
		d.Region = strings.TrimSpace(region)
	}
	d.Stack = strings.TrimSpace(d.Stack)
	d.Partition = strings.TrimSpace(d.Partition)
	d.Intent = strings.TrimSpace(d.Intent)
	if d.Intent == "" && d.Stack != "" {
		d.Intent = Slug(d.Stack)
	}
}

// Slug converts a CloudFormation stack name to a Guardian intent name. It
// splits CamelCase into words and normalizes separators.
func Slug(value string) string {
	runes := []rune(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isUpper && !isLower && !isDigit {
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
			continue
		}
		if isUpper && i > 0 && !prevDash {
			prev := runes[i-1]
			prevLower := prev >= 'a' && prev <= 'z'
			prevDigit := prev >= '0' && prev <= '9'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || prevDigit || (prev >= 'A' && prev <= 'Z' && nextLower) {
				b.WriteByte('-')
			}
		}
		b.WriteRune(r)
		prevDash = false
	}
	out := strings.Trim(strings.ToLower(b.String()), "-")
	if out == "" {
		return "stack"
	}
	return out
}
