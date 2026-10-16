package awssetup

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeTrustPrincipals(t *testing.T) {
	out := normalizeTrustPrincipals([]string{
		" 123456789012 ",
		"123456789012",
		"arn:aws:iam::999999999999:role/tools-pusher",
		"",
	})
	if len(out) != 2 {
		t.Fatalf("principals = %v", out)
	}
	if out[0] != "arn:aws:iam::123456789012:root" {
		t.Fatalf("account root = %q", out[0])
	}
	if out[1] != "arn:aws:iam::999999999999:role/tools-pusher" {
		t.Fatalf("role arn = %q", out[1])
	}
}

func TestBuildTrustPolicy(t *testing.T) {
	raw, err := buildTrustPolicy("111122223333", []string{"arn:aws:iam::444455556666:role/pusher"})
	if err != nil {
		t.Fatalf("buildTrustPolicy: %v", err)
	}
	var policy trustPolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if len(policy.Statement) != 2 {
		t.Fatalf("statements = %d, want 2", len(policy.Statement))
	}
	if principal, _ := policy.Statement[0].Principal["AWS"].(string); principal != "arn:aws:iam::111122223333:root" {
		t.Fatalf("account statement = %+v", policy.Statement[0])
	}
	if principal, _ := policy.Statement[1].Principal["AWS"].(string); principal != "arn:aws:iam::444455556666:role/pusher" {
		t.Fatalf("extra statement = %+v", policy.Statement[1])
	}
	if !strings.Contains(raw, "sts:AssumeRole") {
		t.Fatalf("policy missing action: %s", raw)
	}
}

func TestAppendTrustPrincipalDedupes(t *testing.T) {
	var policy trustPolicy
	if err := json.Unmarshal([]byte(`{
	  "Version": "2012-10-17",
	  "Statement": [
	    {"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::1:root"}, "Action": "sts:AssumeRole"},
	    {"Effect": "Allow", "Principal": {"AWS": ["arn:aws:iam::2:root", "arn:aws:iam::3:root"]}, "Action": "sts:AssumeRole"}
	  ]
	}`), &policy); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if appendTrustPrincipal(&policy, "arn:aws:iam::1:root") {
		t.Fatal("existing string principal must not be added")
	}
	if appendTrustPrincipal(&policy, "arn:aws:iam::2:root") {
		t.Fatal("existing list principal must not be added")
	}
	if !appendTrustPrincipal(&policy, "arn:aws:iam::4:root") {
		t.Fatal("missing principal must be added")
	}
	if len(policy.Statement) != 3 {
		t.Fatalf("statements = %d, want 3", len(policy.Statement))
	}
}
