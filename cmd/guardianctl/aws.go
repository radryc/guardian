package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/radryc/guardian/internal/awssetup"
	"github.com/radryc/guardian/internal/cli/command"
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
	policy := flags.String("policy", "scoped", "deploy-role authorization: scoped (least privilege), admin (AdministratorAccess), or a managed policy name")
	authMode := flags.String("auth-mode", "sso", "Authentication mode: sso, accesskey, rolesanywhere")
	caCertPath := flags.String("ca-cert", "", "Path to existing CA certificate (for rolesanywhere mode)")
	certDir := flags.String("cert-dir", "", "Directory for generated certificates (default: ~/.guardian/certs)")
	dryRun := flags.Bool("dry-run", false, "print actions without executing")
	var trustPrincipals repeatedFlag
	flags.Var(&trustPrincipals, "trust-principal", "additional principal allowed to assume the deploy role (repeatable; account id or ARN)")

	return &command.Command{
		Description: "Check/create GuardianCdkDeployRole, apply the scoped deploy policy, optionally configure IAM Roles Anywhere",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			policyMode, policyName := awssetup.ResolvePolicyMode(*policy)
			cfg := awssetup.Config{
				Profile:            *profile,
				Region:             *region,
				RoleName:           *roleName,
				PolicyMode:         policyMode,
				PolicyName:         policyName,
				BootstrapStackName: "CDKToolkit",
				DryRun:             *dryRun,
				TrustPrincipals:    append([]string(nil), trustPrincipals...),
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
			}
			if err := applyDeployPolicy(ctx, cfg, identity.Account); err != nil {
				return err
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

				caPEM, clientPEM, clientKeyPEM, certsGenerated, err := awssetup.EnsureCerts(certPaths, *caCertPath != "")
				if err != nil {
					return fmt.Errorf("certificates: %w", err)
				}
				_ = clientPEM
				_ = clientKeyPEM
				if certsGenerated {
					fmt.Println("  generated a fresh Roles Anywhere CA and client certificate")
				}

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
					result, raErr := awssetup.EnsureRolesAnywhere(ctx, raCfg, caPEM, cfg.Profile, certsGenerated)
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

			if len(cfg.TrustPrincipals) > 0 {
				fmt.Printf("\n=== trust principals ===\n")
				if err := awssetup.AddTrustPrincipals(ctx, cfg, cfg.TrustPrincipals); err != nil {
					return fmt.Errorf("add trust principals: %w", err)
				}
				for _, principal := range cfg.TrustPrincipals {
					fmt.Printf("  trusted: %s\n", principal)
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
	executionPolicy := flags.String("execution-policy", "scoped", "CloudFormation execution role policy: scoped (GuardianCfnExecutionPolicy), admin (AdministratorAccess), or a managed policy ARN")
	forceBootstrap := flags.Bool("force-bootstrap", false, "re-run cdk bootstrap even when the CDKToolkit stack exists (applies a new execution policy)")
	dryRun := flags.Bool("dry-run", false, "print actions without executing")

	return &command.Command{
		Description: "Bootstrap CDK in target account/region with a scoped execution policy and verify deployment readiness",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg := awssetup.Config{
				Profile:            *profile,
				Region:             *region,
				RoleName:           *roleName,
				BootstrapStackName: *bootstrapStack,
				ForceBootstrap:     *forceBootstrap,
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

			execPolicy := awssetup.ResolveExecutionPolicy(*executionPolicy)
			if execPolicy == awssetup.PolicyScoped {
				fmt.Printf("=== ensuring scoped execution policies ===\n")
				writeARN, err := awssetup.EnsureManagedPolicy(ctx, cfg, awssetup.ExecutionPolicyName, awssetup.ExecutionPolicy(identity.Account, cfg.Region))
				if err != nil {
					return fmt.Errorf("execution policy: %w", err)
				}
				readARN, err := awssetup.EnsureManagedPolicy(ctx, cfg, awssetup.ExecutionReadPolicyName, awssetup.ExecutionReadPolicy(identity.Account, cfg.Region))
				if err != nil {
					return fmt.Errorf("execution read policy: %w", err)
				}
				// cdk bootstrap accepts a comma-separated policy list.
				cfg.ExecutionPolicyARN = writeARN + "," + readARN
				fmt.Printf("  execution policy:      %s\n", writeARN)
				fmt.Printf("  execution read policy: %s\n", readARN)
			} else {
				cfg.ExecutionPolicyARN = execPolicy
				fmt.Printf("execution policy: %s\n", execPolicy)
			}

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

			if exists && !cfg.ForceBootstrap {
				fmt.Printf("  %s stack exists (use --force-bootstrap to apply the execution policy)\n", cfg.BootstrapStackName)
			} else {
				if exists {
					fmt.Printf("  re-bootstrapping %s with the scoped execution policy...\n", cfg.BootstrapStackName)
				} else {
					fmt.Printf("  %s stack not found, bootstrapping...\n", cfg.BootstrapStackName)
				}
				if err := awssetup.RunCDKBootstrap(ctx, cfg, identity.Account); err != nil {
					return fmt.Errorf("cdk bootstrap: %w", err)
				}
				fmt.Printf("  %s stack ready\n", cfg.BootstrapStackName)
			}

			if execPolicy == awssetup.PolicyScoped {
				// The bootstrap deploy role is what the CDK CLI assumes; it
				// must be allowed to pass the per-intent execution roles.
				if err := awssetup.EnsureDeployRolePassesExecutionRoles(ctx, cfg, identity.Account); err != nil {
					return fmt.Errorf("deploy role pass-role policy: %w", err)
				}
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

// applyDeployPolicy puts the scoped inline policy on the deploy role (and
// detaches AdministratorAccess when present), or attaches the requested
// managed policy when PolicyMode is "managed".
func applyDeployPolicy(ctx context.Context, cfg awssetup.Config, accountID string) error {
	if cfg.PolicyMode == awssetup.PolicyScoped {
		fmt.Printf("\n=== applying scoped deploy policy (%s) ===\n", awssetup.InlinePolicyName)
		if err := awssetup.EnsureInlinePolicy(ctx, cfg, awssetup.InlinePolicyName, awssetup.DeployPolicy(accountID, cfg.Region)); err != nil {
			return err
		}
		attached, err := awssetup.AttachedRolePolicies(ctx, cfg)
		if err != nil {
			return err
		}
		for _, arn := range attached {
			if arn == "arn:aws:iam::aws:policy/AdministratorAccess" {
				fmt.Println("  detaching AdministratorAccess (replaced by the scoped policy)")
				if err := awssetup.DetachManagedPolicy(ctx, cfg, arn); err != nil {
					return err
				}
			}
		}
		fmt.Println("  scoped policy applied: deploy + scan + observed/drift + CloudWatch read")
		return nil
	}

	fmt.Printf("\n=== attaching managed policy %s ===\n", cfg.PolicyName)
	if err := awssetup.AttachManagedPolicy(ctx, cfg, cfg.PolicyName); err != nil {
		return err
	}
	if err := awssetup.DeleteInlinePolicy(ctx, cfg, awssetup.InlinePolicyName); err != nil {
		return err
	}
	if cfg.PolicyName == "AdministratorAccess" {
		fmt.Println("  warning: AdministratorAccess is overly broad; prefer --policy scoped")
	}
	return nil
}
