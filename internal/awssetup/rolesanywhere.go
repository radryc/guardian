package awssetup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	ratypes "github.com/aws/aws-sdk-go-v2/service/rolesanywhere/types"
)

type CertPaths struct {
	CADir     string
	CA        string
	Client    string
	ClientKey string
}

func DefaultCertPaths() CertPaths {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".guardian", "certs")
	return CertPaths{
		CADir:     base,
		CA:        filepath.Join(base, "ca.pem"),
		Client:    filepath.Join(base, "client.pem"),
		ClientKey: filepath.Join(base, "client-key.pem"),
	}
}

// clientKeyPath returns the client private key path, tolerating callers that
// constructed CertPaths without setting ClientKey.
func (p CertPaths) clientKeyPath() string {
	if strings.TrimSpace(p.ClientKey) != "" {
		return p.ClientKey
	}
	return filepath.Join(p.CADir, "client-key.pem")
}

// certsUsable reports whether the on-disk CA, client certificate and client key
// are present, parseable, unexpired, signed by the CA, and match each other.
// Any failure means the caller must regenerate the whole set.
func certsUsable(paths CertPaths) bool {
	caPEM, err := os.ReadFile(paths.CA)
	if err != nil {
		return false
	}
	clientPEM, err := os.ReadFile(paths.Client)
	if err != nil {
		return false
	}
	keyPEM, err := os.ReadFile(paths.clientKeyPath())
	if err != nil {
		return false
	}

	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil {
		return false
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return false
	}

	clientBlock, _ := pem.Decode(clientPEM)
	if clientBlock == nil {
		return false
	}
	clientCert, err := x509.ParseCertificate(clientBlock.Bytes)
	if err != nil {
		return false
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return false
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return false
	}

	now := time.Now()
	if now.Before(caCert.NotBefore) || now.After(caCert.NotAfter) {
		return false
	}
	if now.Before(clientCert.NotBefore) || now.After(clientCert.NotAfter) {
		return false
	}
	if clientCert.CheckSignatureFrom(caCert) != nil {
		return false
	}
	pub, ok := clientCert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return false
	}
	return key.PublicKey.N.Cmp(pub.N) == 0 && key.PublicKey.E == pub.E
}

func GenerateCA(paths CertPaths) (*x509.Certificate, *rsa.PrivateKey, error) {
	if err := os.MkdirAll(paths.CADir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create cert dir %s: %w", paths.CADir, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA serial number: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "Guardian IAM Roles Anywhere CA",
			Organization: []string{"Guardian"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CA cert: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parse CA cert: %w", err)
	}

	if err := writePEM(paths.CA, "CERTIFICATE", certDER); err != nil {
		return nil, nil, err
	}
	if err := writePEMKey(paths.clientKeyPath(), key); err != nil {
		return nil, nil, err
	}

	fmt.Printf("  CA certificate written to: %s\n", paths.CA)
	fmt.Printf("  CA private key written to: %s\n", paths.clientKeyPath())
	return cert, key, nil
}

func GenerateClientCert(paths CertPaths, caCert *x509.Certificate, caKey *rsa.PrivateKey) (*x509.Certificate, error) {
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate client key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate client serial number: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "guardian-pusher",
			Organization: []string{"Guardian"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(1, 0, 0),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create client cert: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	if err := writePEM(paths.Client, "CERTIFICATE", certDER); err != nil {
		return nil, err
	}
	if err := writePEMKey(paths.clientKeyPath(), clientKey); err != nil {
		return nil, err
	}

	fmt.Printf("  Client certificate written to: %s\n", paths.Client)
	fmt.Printf("  Client key written to: %s\n", paths.clientKeyPath())
	return cert, nil
}

// EnsureCerts returns the Roles Anywhere CA, client certificate and client key,
// regenerating the whole set when any of them is missing, unreadable, expired,
// not signed by the CA, or does not match the client key. generated reports
// whether a new CA was written, which means any existing trust anchor must be
// rebound to the new CA before the credentials can work again.
func EnsureCerts(paths CertPaths, forceNew bool) (caPEM string, clientPEM string, clientKeyPEM string, generated bool, err error) {
	if !forceNew && certsUsable(paths) {
		caBytes, _ := os.ReadFile(paths.CA)
		certBytes, _ := os.ReadFile(paths.Client)
		keyBytes, _ := os.ReadFile(paths.clientKeyPath())
		fmt.Println("  Using existing certificates")
		return string(caBytes), string(certBytes), string(keyBytes), false, nil
	}

	caCert, caKey, err := GenerateCA(paths)
	if err != nil {
		return "", "", "", false, err
	}

	if _, err := GenerateClientCert(paths, caCert, caKey); err != nil {
		return "", "", "", false, err
	}

	caBytes, _ := os.ReadFile(paths.CA)
	clientCertBytes, _ := os.ReadFile(paths.Client)
	clientKeyBytes, _ := os.ReadFile(paths.clientKeyPath())

	return string(caBytes), string(clientCertBytes), string(clientKeyBytes), true, nil
}

type RolesAnywhereConfig struct {
	Profile         string
	Region          string
	TrustAnchorARN  string
	ProfileARN      string
	RoleARN         string
	RoleName        string
	Certificate     string
	CertificatePath string
	PrivateKeyPath  string
	CertDir         string
}

// EnsureRolesAnywhere creates or reuses the GuardianLocalAnchor trust anchor
// and GuardianLocalProfile. When certGenerated is true the local CA was just
// regenerated (for example after a wipe), so an existing trust anchor is
// rebond to the new CA; otherwise a stale anchor would reject the new client
// certificate and the pusher could not obtain credentials.
func EnsureRolesAnywhere(ctx context.Context, cfg RolesAnywhereConfig, caPEM string, sourceProfile string, certGenerated bool) (*RolesAnywhereConfig, error) {
	awsCfg, err := loadAWSRolesAnywhereConfig(ctx, sourceProfile, cfg.Region)
	if err != nil {
		return nil, err
	}
	client := rolesanywhere.NewFromConfig(awsCfg)

	result := &RolesAnywhereConfig{
		Profile:         cfg.Profile,
		Region:          cfg.Region,
		RoleName:        cfg.RoleName,
		RoleARN:         cfg.RoleARN,
		Certificate:     cfg.Certificate,
		CertificatePath: cfg.CertificatePath,
		PrivateKeyPath:  cfg.PrivateKeyPath,
		CertDir:         cfg.CertDir,
	}

	// Reuse the existing anchor instead of creating another one with the same
	// name: repeated setup runs would otherwise strand anchors, and the pusher
	// image or ~/.aws/config built from an earlier run would reference a stale
	// ARN that the role trust policy no longer allows. Update calls take the
	// UUID (TrustAnchorId), not the ARN.
	anchors, err := client.ListTrustAnchors(ctx, &rolesanywhere.ListTrustAnchorsInput{})
	if err != nil {
		return nil, fmt.Errorf("list trust anchors: %w", err)
	}
	for _, ta := range anchors.TrustAnchors {
		if awsroot.ToString(ta.Name) != "GuardianLocalAnchor" {
			continue
		}
		if result.TrustAnchorARN == "" {
			result.TrustAnchorARN = awsroot.ToString(ta.TrustAnchorArn)
			if certGenerated {
				if _, err := client.UpdateTrustAnchor(ctx, &rolesanywhere.UpdateTrustAnchorInput{
					TrustAnchorId: ta.TrustAnchorId,
					Name:          awsroot.String("GuardianLocalAnchor"),
					Source: &ratypes.Source{
						SourceData: &ratypes.SourceDataMemberX509CertificateData{
							Value: caPEM,
						},
						SourceType: ratypes.TrustAnchorTypeCertificateBundle,
					},
				}); err != nil {
					return nil, fmt.Errorf("rebind trust anchor to regenerated CA: %w", err)
				}
				fmt.Println("  trust anchor updated with the regenerated CA certificate")
			}
			continue
		}
		// Heal duplicates left behind by older setup runs: keep the first and
		// remove the rest so selection stays deterministic.
		if _, err := client.DisableTrustAnchor(ctx, &rolesanywhere.DisableTrustAnchorInput{
			TrustAnchorId: ta.TrustAnchorId,
		}); err != nil {
			fmt.Printf("  warning: could not disable duplicate trust anchor %s: %v\n", awsroot.ToString(ta.TrustAnchorArn), err)
			continue
		}
		if _, err := client.DeleteTrustAnchor(ctx, &rolesanywhere.DeleteTrustAnchorInput{
			TrustAnchorId: ta.TrustAnchorId,
		}); err != nil {
			fmt.Printf("  warning: could not delete duplicate trust anchor %s: %v\n", awsroot.ToString(ta.TrustAnchorArn), err)
			continue
		}
		fmt.Printf("  removed duplicate trust anchor %s\n", awsroot.ToString(ta.TrustAnchorArn))
	}
	if result.TrustAnchorARN == "" {
		taOut, err := client.CreateTrustAnchor(ctx, &rolesanywhere.CreateTrustAnchorInput{
			Name: awsroot.String("GuardianLocalAnchor"),
			Source: &ratypes.Source{
				SourceData: &ratypes.SourceDataMemberX509CertificateData{
					Value: caPEM,
				},
				SourceType: ratypes.TrustAnchorTypeCertificateBundle,
			},
			Enabled: awsroot.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("create trust anchor: %w", err)
		}
		result.TrustAnchorARN = awsroot.ToString(taOut.TrustAnchor.TrustAnchorArn)
	}

	roleARNs := []string{cfg.RoleARN}
	profiles, err := client.ListProfiles(ctx, &rolesanywhere.ListProfilesInput{})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	for _, p := range profiles.Profiles {
		if awsroot.ToString(p.Name) != "GuardianLocalProfile" {
			continue
		}
		if result.ProfileARN == "" {
			result.ProfileARN = awsroot.ToString(p.ProfileArn)
			if _, err := client.UpdateProfile(ctx, &rolesanywhere.UpdateProfileInput{
				ProfileId: p.ProfileId,
				Name:      awsroot.String("GuardianLocalProfile"),
				RoleArns:  roleARNs,
			}); err != nil {
				return nil, fmt.Errorf("update profile: %w", err)
			}
			if _, err := client.EnableProfile(ctx, &rolesanywhere.EnableProfileInput{
				ProfileId: p.ProfileId,
			}); err != nil {
				return nil, fmt.Errorf("enable profile: %w", err)
			}
			continue
		}
		if _, err := client.DisableProfile(ctx, &rolesanywhere.DisableProfileInput{
			ProfileId: p.ProfileId,
		}); err != nil {
			fmt.Printf("  warning: could not disable duplicate profile %s: %v\n", awsroot.ToString(p.ProfileArn), err)
			continue
		}
		if _, err := client.DeleteProfile(ctx, &rolesanywhere.DeleteProfileInput{
			ProfileId: p.ProfileId,
		}); err != nil {
			fmt.Printf("  warning: could not delete duplicate profile %s: %v\n", awsroot.ToString(p.ProfileArn), err)
			continue
		}
		fmt.Printf("  removed duplicate profile %s\n", awsroot.ToString(p.ProfileArn))
	}
	if result.ProfileARN == "" {
		profileOut, err := client.CreateProfile(ctx, &rolesanywhere.CreateProfileInput{
			Name:     awsroot.String("GuardianLocalProfile"),
			RoleArns: roleARNs,
			Enabled:  awsroot.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("create profile: %w", err)
		}
		result.ProfileARN = awsroot.ToString(profileOut.Profile.ProfileArn)
	}

	return result, nil
}

func loadAWSRolesAnywhereConfig(ctx context.Context, profile, region string) (awsroot.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	} else if prof := os.Getenv("AWS_PROFILE"); prof != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(prof))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

func PrintRolesAnywhereSummary(cfg RolesAnywhereConfig) {
	home, _ := os.UserHomeDir()
	configTarget := filepath.Join(home, ".aws", "config")

	fmt.Println()
	fmt.Println("=== IAM Roles Anywhere configured ===")
	fmt.Printf("  Trust Anchor: %s\n", cfg.TrustAnchorARN)
	fmt.Printf("  Profile:      %s\n", cfg.ProfileARN)
	fmt.Printf("  Role:         %s\n", cfg.RoleARN)
	fmt.Printf("  Certificates: %s/\n", cfg.CertDir)
	fmt.Println()

	profileBlock := fmt.Sprintf(`
[profile guardian-rolesanywhere]
credential_process = aws_signing_helper credential-process --certificate %s --private-key %s --trust-anchor-arn %s --profile-arn %s --role-arn %s
region = %s
`,
		cfg.CertificatePath,
		cfg.PrivateKeyPath,
		cfg.TrustAnchorARN,
		cfg.ProfileARN,
		cfg.RoleARN,
		cfg.Region,
	)

	fmt.Println("Adding to ~/.aws/config:")
	fmt.Println(profileBlock)

	if err := appendAWSConfig(configTarget, profileBlock); err != nil {
		fmt.Printf("  warning: could not write config: %v\n", err)
	} else {
		fmt.Printf("  written to %s\n", configTarget)
	}

	fmt.Println("  Verify credential process:")
	fmt.Printf("    AWS_PROFILE=guardian-rolesanywhere aws sts get-caller-identity\n")
}

func appendAWSConfig(path, block string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	existing, _ := os.ReadFile(path)
	content := string(existing)

	if strings.Contains(content, "[profile guardian-rolesanywhere]") {
		start := strings.Index(content, "[profile guardian-rolesanywhere]")
		end := strings.Index(content[start:], "\n[")
		if end == -1 {
			end = len(content) - start
		}
		content = strings.TrimRight(content[:start]+content[start+end:], "\n")
	}

	f, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(content) > 0 {
		if _, err := f.WriteString(content + "\n\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString(block); err != nil {
		return err
	}
	return nil
}

func writePEM(path, pemType string, der []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: der})
}

func writePEMKey(path string, key *rsa.PrivateKey) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return pem.Encode(f, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}
