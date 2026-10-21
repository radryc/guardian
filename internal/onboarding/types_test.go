package onboarding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakePage struct {
	params []ssmtypes.Parameter
	next   string
}

type fakeSSM struct {
	params []ssmtypes.Parameter
	pages  []fakePage
	calls  int
}

func (f *fakeSSM) GetParametersByPath(_ context.Context, _ *ssm.GetParametersByPathInput, _ ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	f.calls++
	if len(f.pages) > 0 {
		page := f.pages[0]
		f.pages = f.pages[1:]
		out := &ssm.GetParametersByPathOutput{Parameters: page.params}
		if page.next != "" {
			out.NextToken = aws.String(page.next)
		}
		return out, nil
	}
	return &ssm.GetParametersByPathOutput{Parameters: f.params}, nil
}

func TestSSMReaderParsesAndDefaults(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"stack":     "CustomerAppStack",
		"partition": "customer-prod-1",
		"repo": map[string]any{
			"url": "https://github.com/acme/app.git",
		},
		"probes": []map[string]any{{
			"name": "health",
			"url":  "https://api.example.com/healthz",
		}},
	})
	client := &fakeSSM{params: []ssmtypes.Parameter{{
		Name:  aws.String("/guardian/onboarding/CustomerAppStack"),
		Value: aws.String(string(raw)),
	}}}
	reader := &SSMReader{Client: client, Prefix: "/guardian/onboarding/", Account: "123", Region: "eu-west-1"}
	got, err := reader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("descriptors = %d, want 1", len(got))
	}
	if got[0].Account != "123" || got[0].Region != "eu-west-1" {
		t.Fatalf("defaults = %q/%q", got[0].Account, got[0].Region)
	}
	if got[0].Intent != "customer-app-stack" {
		t.Fatalf("intent = %q", got[0].Intent)
	}
	if got[0].APIVersion != APIVersion || got[0].Kind != Kind {
		t.Fatalf("apiVersion/kind = %q/%q", got[0].APIVersion, got[0].Kind)
	}
}

func TestSSMReaderPagination(t *testing.T) {
	first, _ := json.Marshal(map[string]any{"stack": "A", "partition": "p"})
	second, _ := json.Marshal(map[string]any{"stack": "B", "partition": "p"})
	client := &fakeSSM{pages: []fakePage{
		{params: []ssmtypes.Parameter{{Name: aws.String("a"), Value: aws.String(string(first))}}, next: "next"},
		{params: []ssmtypes.Parameter{{Name: aws.String("b"), Value: aws.String(string(second))}}},
	}}
	reader := &SSMReader{Client: client, Prefix: "/guardian/onboarding", Account: "1", Region: "r"}
	got, err := reader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("descriptors = %d, want 2", len(got))
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want 2", client.calls)
	}
}

func TestSSMReaderSkipsInvalid(t *testing.T) {
	client := &fakeSSM{params: []ssmtypes.Parameter{
		{Name: aws.String("bad"), Value: aws.String("not json")},
	}}
	reader := &SSMReader{Client: client, Prefix: "/guardian/onboarding"}
	if _, err := reader.List(context.Background()); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"CustomerAppStack": "customer-app-stack",
		"my_stack":         "my-stack",
		"":                 "stack",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
