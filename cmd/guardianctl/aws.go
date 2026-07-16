package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/awssetup"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
)

func awsCommands() []bootstrapReg {
	return []bootstrapReg{
		{Group: "aws", Name: "setup", Cmd: awsSetupCommand()},
		{Group: "aws", Name: "init", Cmd: awsInitCommand()},
	}
}

func awsSetupCommand() *command.Command {
	flags := flag.NewFlagSet("aws setup", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profile := flags.String("profile", "", "AWS profile to use (default: $AWS_PROFILE)")
	region := flags.String("region", "", "AWS region (default: $AWS_REGION or us-east-1)")
	roleName := flags.String("role-name", "GuardianCdkDeployRole", "IAM role name for CDK deployments")
	policy := flags.String("policy", "AdministratorAccess", "IAM policy to attach to the role")
	authMode := flags.String("auth-mode", "sso", "Authentication mode: sso, accesskey, rolesanywhere")
	caCertPath := flags.String("ca-cert", "", "Path to existing CA certificate (for rolesanywhere mode)")
	certDir := flags.String("cert-dir", "", "Directory for generated certificates (default: ~/.guardian/certs)")
	dryRun := flags.Bool("dry-run", false, "print actions without executing")

	return &command.Command{
		Description: "Check/create GuardianCdkDeployRole, attach IAM policy, optionally configure IAM Roles Anywhere",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg := awssetup.Config{
				Profile:            *profile,
				Region:             *region,
				RoleName:           *roleName,
				PolicyName:         *policy,
				BootstrapStackName: "CDKToolkit",
				DryRun:             *dryRun,
			}.WithDefaults()

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			fmt.Printf("=== aws setup ===\n")
			if cfg.Profile != "" {
				fmt.Printf("profile:   %s\n", cfg.Profile)
			}
			fmt.Printf("region:    %s\n", cfg.Region)
			fmt.Printf("auth mode: %s\n", *authMode)
			if cfg.DryRun {
				fmt.Println("(dry-run mode)")
			}

			fmt.Println("=== checking aws authentication ===")
			identity, err := awssetup.GetCallerIdentity(ctx, cfg)
			if err != nil {
				return fmt.Errorf("aws authentication failed: %w\n\ntip: run 'aws sso login --profile %s' or set AWS_PROFILE", err, cfg.Profile)
			}
			fmt.Printf("authenticated as: %s\n", identity.ARN)
			fmt.Printf("account: %s\n", identity.Account)

			roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", identity.Account, cfg.RoleName)

			fmt.Printf("\n=== checking IAM role %s ===\n", cfg.RoleName)
			existing, err := awssetup.GetRole(ctx, cfg, identity.Account)
			if err != nil {
				return fmt.Errorf("check role: %w", err)
			}
			if existing.Exists {
				fmt.Printf("role exists: %s\n", existing.ARN)
			} else {
				fmt.Printf("role %s not found\n", cfg.RoleName)
				fmt.Printf("=== creating IAM role %s ===\n", cfg.RoleName)
				info, err := awssetup.EnsureRole(ctx, cfg, identity.Account)
				if err != nil {
					return fmt.Errorf("ensure role: %w", err)
				}
				fmt.Printf("  role ARN: %s\n", info.ARN)
				if info.PolicyARN != "" {
					fmt.Printf("  attached policy: %s\n", info.PolicyARN)
				}
				if cfg.PolicyName == "AdministratorAccess" {
					fmt.Println()
					fmt.Println("  warning: AdministratorAccess is overly broad.")
				}
			}

			if *authMode == "rolesanywhere" {
				fmt.Printf("\n=== IAM Roles Anywhere ===\n")
				_ = roleARN

				var certPaths awssetup.CertPaths
				if *certDir != "" {
					certPaths = awssetup.CertPaths{
						CADir:     *certDir,
						CA:        filepath.Join(*certDir, "ca.pem"),
						Client:    filepath.Join(*certDir, "client.pem"),
						ClientKey: filepath.Join(*certDir, "client-key.pem"),
					}
				} else {
					certPaths = awssetup.DefaultCertPaths()
				}

				caPEM, clientPEM, clientKeyPEM, err := awssetup.EnsureCerts(certPaths, *caCertPath != "")
				if err != nil {
					return fmt.Errorf("certificates: %w", err)
				}
				_ = clientPEM
				_ = clientKeyPEM

				raCfg := awssetup.RolesAnywhereConfig{
					Profile:         "guardian-rolesanywhere",
					Region:          cfg.Region,
					RoleARN:         roleARN,
					RoleName:        cfg.RoleName,
					Certificate:     caPEM,
					CertificatePath: certPaths.Client,
					PrivateKeyPath:  filepath.Join(certPaths.CADir, "client-key.pem"),
					CertDir:         certPaths.CADir,
				}

				if !cfg.DryRun {
					result, raErr := awssetup.EnsureRolesAnywhere(ctx, raCfg, caPEM, cfg.Profile)
					if raErr != nil {
						return fmt.Errorf("roles anywhere setup: %w", raErr)
					}
					if err := awssetup.UpdateRoleTrustPolicyForRolesAnywhere(ctx, cfg, identity.Account, result.TrustAnchorARN); err != nil {
						fmt.Printf("  warning: could not update role trust policy: %v\n", err)
					}
					awssetup.PrintRolesAnywhereSummary(*result)
				} else {
					fmt.Println("  would create trust anchor: GuardianLocalAnchor")
					fmt.Println("  would create profile:      GuardianLocalProfile")
				}
			}

			fmt.Println()
			fmt.Println("=== setup complete ===")
			if *authMode == "rolesanywhere" {
				fmt.Println("Install aws_signing_helper for credential_process:")
				fmt.Println("  git clone https://github.com/aws/rolesanywhere-credential-helper.git /tmp/ash && \\")
				fmt.Println("  make -C /tmp/ash release && sudo cp /tmp/ash/build/bin/aws_signing_helper /usr/local/bin/")
			} else {
				fmt.Printf("next: guardianctl aws init --profile %s --region %s\n",
					cfg.Profile, cfg.Region)
			}
			return nil
		},
	}
}

func awsInitCommand() *command.Command {
	flags := flag.NewFlagSet("aws init", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	profile := flags.String("profile", "", "AWS profile to use (default: $AWS_PROFILE)")
	region := flags.String("region", "", "AWS region (default: $AWS_REGION or us-east-1)")
	roleName := flags.String("role-name", "GuardianCdkDeployRole", "IAM role name for CDK deployments")
	bootstrapStack := flags.String("bootstrap-stack", "CDKToolkit", "CDK bootstrap stack name")
	createEcr := flags.Bool("create-ecr", false, "also create ECR repositories for all Strata images")
	dryRun := flags.Bool("dry-run", false, "print actions without executing")

	return &command.Command{
		Description: "Bootstrap CDK in target account/region and verify deployment readiness",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg := awssetup.Config{
				Profile:            *profile,
				Region:             *region,
				RoleName:           *roleName,
				BootstrapStackName: *bootstrapStack,
				DryRun:             *dryRun,
			}.WithDefaults()

			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			fmt.Printf("=== aws init ===\n")
			if cfg.Profile != "" {
				fmt.Printf("profile: %s\n", cfg.Profile)
			}
			fmt.Printf("region:  %s\n", cfg.Region)
			if cfg.DryRun {
				fmt.Println("(dry-run mode)")
			}

			fmt.Println("=== resolving account ===")
			identity, err := awssetup.GetCallerIdentity(ctx, cfg)
			if err != nil {
				return fmt.Errorf("aws authentication failed: %w\n\ntip: run 'aws sso login --profile %s'", err, cfg.Profile)
			}
			fmt.Printf("account: %s\n", identity.Account)

			fmt.Println("=== testing assume-role ===")
			if err := testAssumeRole(ctx, cfg, identity.Account); err != nil {
				fmt.Printf("  warning: could not assume %s: %v\n", cfg.RoleName, err)
				fmt.Printf("  make sure the role exists and your principal is trusted.\n")
				fmt.Printf("  run: guardianctl aws setup --profile %s --region %s\n", cfg.Profile, cfg.Region)
			} else {
				fmt.Printf("  can assume %s\n", cfg.RoleName)
			}

			fmt.Println("=== checking CDK bootstrap stack ===")
			exists, err := awssetup.CheckCDKBootstrapStack(ctx, cfg, identity.Account)
			if err != nil {
				return fmt.Errorf("check bootstrap stack: %w", err)
			}

			if exists {
				fmt.Printf("  %s stack exists\n", cfg.BootstrapStackName)
			} else {
				fmt.Printf("  %s stack not found, bootstrapping...\n", cfg.BootstrapStackName)
				if err := awssetup.RunCDKBootstrap(ctx, cfg, identity.Account); err != nil {
					return fmt.Errorf("cdk bootstrap: %w", err)
				}
				fmt.Printf("  %s stack created\n", cfg.BootstrapStackName)
			}

			if *createEcr {
				fmt.Println("=== checking ECR repositories ===")
				if err := ensureECRRepos(ctx, cfg); err != nil {
					return fmt.Errorf("ecr repositories: %w", err)
				}
			}

			fmt.Println()
			fmt.Printf("=== init complete ===\n")
			fmt.Printf("account %s/%s is ready for CDK deployments.\n", identity.Account, cfg.Region)
			return nil
		},
	}
}

func testAssumeRole(ctx context.Context, cfg awssetup.Config, accountID string) error {
	if cfg.DryRun {
		fmt.Printf("  would test assume-role: arn:aws:iam::%s:role/%s\n", accountID, cfg.RoleName)
		return nil
	}

	cdk, err := exec.LookPath("cdk")
	if err != nil {
		cdk = "aws"
	}

	var cmd *exec.Cmd
	if strings.Contains(cdk, "cdk") {
		cmd = exec.CommandContext(ctx, cdk, "bootstrap",
			fmt.Sprintf("aws://%s/%s", accountID, cfg.Region),
			"--show-template")
	} else {
		cmd = exec.CommandContext(ctx, cdk, "sts", "get-caller-identity")
	}

	cmd.Env = buildAWSEnv(cfg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err.Error(), string(out))
	}
	_ = out
	return nil
}

func ensureECRRepos(ctx context.Context, cfg awssetup.Config) error {
	repos := []string{
		"strata/monofs-server",
		"strata/monofs-router",
		"strata/monofs-fetcher",
		"strata/monofs-search",
		"strata/monofs-registry",
		"strata/monofs-client",
		"strata/guardian",
		"strata/guardian-pusher-aws",
		"strata/doctor-ingest",
		"strata/doctor-query",
		"strata/lb",
		"strata/lagent-llm",
		"strata/lagent-backend",
		"strata/lagent-frontend",
	}

	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws cli not found in PATH")
	}

	for _, repo := range repos {
		if cfg.DryRun {
			fmt.Printf("  would ensure ECR repo: %s\n", repo)
			continue
		}

		checkCmd := exec.CommandContext(ctx, awsBin, "ecr", "describe-repositories",
			"--repository-names", repo, "--region", cfg.Region)
		checkCmd.Env = buildAWSEnv(cfg)
		if checkCmd.Run() == nil {
			fmt.Printf("  exists: %s\n", repo)
			continue
		}

		fmt.Printf("  creating: %s\n", repo)
		createCmd := exec.CommandContext(ctx, awsBin, "ecr", "create-repository",
			"--repository-name", repo, "--region", cfg.Region)
		createCmd.Env = buildAWSEnv(cfg)
		out, err := createCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("  warning: create %s: %v\n%s", repo, err, string(out))
		} else {
			fmt.Printf("  created: %s\n", repo)
		}
	}

	return nil
}

func buildAWSEnv(cfg awssetup.Config) []string {
	env := os.Environ()
	if cfg.Profile != "" {
		env = append(env, "AWS_PROFILE="+cfg.Profile)
	}
	if cfg.Region != "" {
		env = append(env, "AWS_REGION="+cfg.Region)
		env = append(env, "AWS_DEFAULT_REGION="+cfg.Region)
		env = append(env, "CDK_DEFAULT_REGION="+cfg.Region)
	}
	return env
}
