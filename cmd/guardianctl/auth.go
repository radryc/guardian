package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/clitoken"
)

func authEnvOr(keys []string, fallback string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return fallback
}

func loginCommand() *command.Command {
	flags := flag.NewFlagSet("auth login", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	issuer := flags.String("issuer", authEnvOr([]string{"GUARDIAN_OIDC_ISSUER", "MONOFS_OIDC_ISSUER"}, ""), "OIDC issuer URL")
	clientID := flags.String("client-id", authEnvOr([]string{"MONOFS_OIDC_CLIENT_ID"}, "monofs"), "OAuth client id")
	clientSecret := flags.String("client-secret", authEnvOr([]string{"MONOFS_OIDC_CLIENT_SECRET"}, ""), "OAuth client secret (for confidential clients)")
	return &command.Command{
		Description: "Log in via OIDC device flow and cache the token for CLI calls",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			if strings.TrimSpace(*issuer) == "" {
				return fmt.Errorf("--issuer (or GUARDIAN_OIDC_ISSUER / MONOFS_OIDC_ISSUER) is required")
			}
			eps, err := coreauthz.DiscoverEndpoints(ctx, *issuer, nil)
			if err != nil {
				return fmt.Errorf("discover OIDC endpoints: %w", err)
			}
			if strings.TrimSpace(eps.DeviceAuthURL) == "" {
				return fmt.Errorf("issuer %q does not advertise a device authorization endpoint", *issuer)
			}
			client := coreauthz.NewDeviceFlowClient(*clientID, []string{"openid", "email", "profile"}, eps)
			client.ClientSecret = *clientSecret

			dev, err := client.Authorize(ctx)
			if err != nil {
				return fmt.Errorf("start device login: %w", err)
			}
			verify := dev.VerificationURIComplete
			if verify == "" {
				verify = dev.VerificationURI
			}
			fmt.Printf("\nTo sign in, open:\n  %s\nand enter code: %s\n\nWaiting for approval...\n", verify, dev.UserCode)

			tok, err := client.Poll(ctx, dev)
			if err != nil {
				return fmt.Errorf("device login: %w", err)
			}
			if err := clitoken.Save(tok); err != nil {
				return fmt.Errorf("cache token: %w", err)
			}
			fmt.Printf("✓ logged in; token cached at %s\n", clitoken.TokenPath())
			return nil
		},
	}
}

func logoutCommand() *command.Command {
	flags := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	return &command.Command{
		Description: "Remove the cached CLI login token",
		Flags:       flags,
		Run: func(_ context.Context, _ []string) error {
			if err := clitoken.Delete(); err != nil {
				return err
			}
			fmt.Println("✓ logged out")
			return nil
		},
	}
}

func whoamiCommand() *command.Command {
	flags := flag.NewFlagSet("auth whoami", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	return &command.Command{
		Description: "Show the identity from the cached CLI token",
		Flags:       flags,
		Run: func(_ context.Context, _ []string) error {
			tok := clitoken.Bearer()
			if tok == "" {
				return fmt.Errorf("not logged in (run: guardianctl auth login)")
			}
			claims, err := decodeJWTClaims(tok)
			if err != nil {
				fmt.Println("token present (opaque; cannot decode claims)")
				return nil
			}
			sub, _ := claims["sub"].(string)
			email, _ := claims["email"].(string)
			fmt.Printf("subject: %s\nemail:   %s\n", sub, email)
			return nil
		},
	}
}

// decodeJWTClaims decodes the payload of a JWT without verifying its signature.
func decodeJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}
