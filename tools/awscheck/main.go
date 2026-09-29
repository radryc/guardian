package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		fmt.Println("load config error:", err)
		os.Exit(1)
	}
	fmt.Println("region:", cfg.Region)

	fmt.Println("--- direct SSO creds ---")
	if creds, err := cfg.Credentials.Retrieve(ctx); err != nil {
		fmt.Println("direct retrieve error:", err)
	} else {
		fmt.Println("direct source:", creds.Source)
	}

	assume := os.Getenv("ASSUME_ROLE_ARN")
	if assume == "" {
		assume = "arn:aws:iam::975049940689:role/GuardianCdkDeployRole"
	}
	fmt.Println("--- assume role:", assume, "---")
	provider := stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), assume, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = "guardian-aws-scan"
	})
	cfg.Credentials = aws.NewCredentialsCache(provider)
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		fmt.Println("assume retrieve error:", err)
		os.Exit(1)
	}
	fmt.Println("assumed source:", creds.Source, "akid:", creds.AccessKeyID)

	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		fmt.Println("sts error:", err)
		os.Exit(1)
	}
	fmt.Println("identity:", *out.Arn)
}
