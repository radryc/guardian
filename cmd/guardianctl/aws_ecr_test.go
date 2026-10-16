package main

import "testing"

func TestECRRegistryHost(t *testing.T) {
	if got, want := ecrRegistryHost("123456789012", "eu-west-1"), "123456789012.dkr.ecr.eu-west-1.amazonaws.com"; got != want {
		t.Fatalf("registry host = %q, want %q", got, want)
	}
	if got, want := ecrRegistryHost(" 123 ", " us-east-1 "), "123.dkr.ecr.us-east-1.amazonaws.com"; got != want {
		t.Fatalf("trimmed host = %q, want %q", got, want)
	}
}

func TestProfileArg(t *testing.T) {
	if got := profileArg(""); got != "" {
		t.Fatalf("empty profile = %q", got)
	}
	if got := profileArg("strata-demo-1"); got != " --profile strata-demo-1" {
		t.Fatalf("profile arg = %q", got)
	}
}

func TestECRAccountIDDryRun(t *testing.T) {
	account, err := ecrAccountID(nil, "", "", true)
	if err != nil {
		t.Fatalf("dry-run account: %v", err)
	}
	if account != "000000000000" {
		t.Fatalf("dry-run account = %q", account)
	}
}
