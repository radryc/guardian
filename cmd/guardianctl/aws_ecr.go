package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
)

// awsEcrLoginCommand logs Docker in to a target account's ECR registry, which
// the build & deploy demo needs before pushing an image.
func awsEcrLoginCommand() *command.Command {
	flags := flag.NewFlagSet("aws ecr-login", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profile := flags.String("profile", "", "AWS profile (default: $AWS_PROFILE)")
	region := flags.String("region", "", "AWS region (default: $AWS_REGION or us-east-1)")
	registry := flags.String("registry", "", "registry host (default: <account>.dkr.ecr.<region>.amazonaws.com)")
	dryRun := flags.Bool("dry-run", false, "print the commands without executing them")
	return &command.Command{Description: "Log Docker in to an AWS ECR registry", Flags: flags, Run: func(ctx context.Context, args []string) error {
		awsProfile := firstNonEmpty(*profile, os.Getenv("AWS_PROFILE"))
		awsRegion := firstNonEmpty(*region, os.Getenv("AWS_REGION"), "us-east-1")
		registryHost := strings.TrimSpace(*registry)
		if registryHost == "" {
			account, err := ecrAccountID(ctx, awsProfile, awsRegion, *dryRun)
			if err != nil {
				return err
			}
			registryHost = ecrRegistryHost(account, awsRegion)
		}
		if *dryRun {
			fmt.Fprintf(os.Stderr, "+ aws ecr get-login-password --region %s%s | docker login --username AWS --password-stdin %s\n",
				awsRegion, profileArg(awsProfile), registryHost)
			return nil
		}
		password, err := ecrLoginPassword(ctx, awsProfile, awsRegion)
		if err != nil {
			return err
		}
		login := exec.CommandContext(ctx, "docker", "login", "--username", "AWS", "--password-stdin", registryHost)
		login.Stdin = strings.NewReader(password)
		login.Stdout = os.Stdout
		login.Stderr = os.Stderr
		if err := login.Run(); err != nil {
			return fmt.Errorf("docker login %s: %w", registryHost, err)
		}
		return nil
	}}
}

// ecrRegistryHost builds the ECR registry host for an account and region.
func ecrRegistryHost(account, region string) string {
	return fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", strings.TrimSpace(account), strings.TrimSpace(region))
}

func profileArg(profile string) string {
	if strings.TrimSpace(profile) == "" {
		return ""
	}
	return " --profile " + strings.TrimSpace(profile)
}

func ecrAccountID(ctx context.Context, profile, region string, dryRun bool) (string, error) {
	if dryRun {
		return "000000000000", nil
	}
	args := []string{"sts", "get-caller-identity", "--query", "Account", "--output", "text"}
	if strings.TrimSpace(region) != "" {
		args = append(args, "--region", strings.TrimSpace(region))
	}
	if strings.TrimSpace(profile) != "" {
		args = append(args, "--profile", strings.TrimSpace(profile))
	}
	out, err := exec.CommandContext(ctx, "aws", args...).Output()
	if err != nil {
		return "", fmt.Errorf("aws sts get-caller-identity: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func ecrLoginPassword(ctx context.Context, profile, region string) (string, error) {
	args := []string{"ecr", "get-login-password", "--region", strings.TrimSpace(region)}
	if strings.TrimSpace(profile) != "" {
		args = append(args, "--profile", strings.TrimSpace(profile))
	}
	out, err := exec.CommandContext(ctx, "aws", args...).Output()
	if err != nil {
		return "", fmt.Errorf("aws ecr get-login-password: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
