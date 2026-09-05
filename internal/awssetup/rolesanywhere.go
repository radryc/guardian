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
	CADir    string
	CA       string
	Client   string
	ClientKey string
}

func DefaultCertPaths() CertPaths {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".guardian", "certs")
	return CertPaths{
		CADir:      base,
		CA:         filepath.Join(base, "ca.pem"),
		Client:     filepath.Join(base, "client.pem"),
		ClientKey:  filepath.Join(base, "client.key"),
	}
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
	if err := writePEMKey(paths.ClientKey, key); err != nil {
		return nil, nil, err
	}

	fmt.Printf("  CA certificate written to: %s\n", paths.CA)
	fmt.Printf("  CA private key written to: %s\n", paths.ClientKey)
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

	clientKeyPath := filepath.Join(paths.CADir, "client-key.pem")
	if err := writePEM(paths.Client, "CERTIFICATE", certDER); err != nil {
		return nil, err
	}
	if err := writePEMKey(clientKeyPath, clientKey); err != nil {
		return nil, err
	}

	fmt.Printf("  Client certificate written to: %s\n", paths.Client)
	fmt.Printf("  Client key written to: %s\n", clientKeyPath)
	return cert, nil
}

func EnsureCerts(paths CertPaths, forceNew bool) (caPEM string, clientPEM string, clientKeyPEM string, err error) {
	if !forceNew {
		if caBytes, e := os.ReadFile(paths.CA); e == nil {
			caPEM = string(caBytes)
		}
		if certBytes, e := os.ReadFile(paths.Client); e == nil {
			clientPEM = string(certBytes)
		}
		clientKeyPath := filepath.Join(paths.CADir, "client-key.pem")
		if keyBytes, e := os.ReadFile(clientKeyPath); e == nil {
			clientKeyPEM = string(keyBytes)
		}
		if caPEM != "" && clientPEM != "" && clientKeyPEM != "" {
			fmt.Println("  Using existing certificates")
			return
		}
	}

	caCert, caKey, err := GenerateCA(paths)
	if err != nil {
		return "", "", "", err
	}

	_, err = GenerateClientCert(paths, caCert, caKey)
	if err != nil {
		return "", "", "", err
	}

	caBytes, _ := os.ReadFile(paths.CA)
	clientCertBytes, _ := os.ReadFile(paths.Client)
	clientKeyPath := filepath.Join(paths.CADir, "client-key.pem")
	clientKeyBytes, _ := os.ReadFile(clientKeyPath)

	return string(caBytes), string(clientCertBytes), string(clientKeyBytes), nil
}

type RolesAnywhereConfig struct {
	Profile          string
	Region           string
	TrustAnchorARN   string
	ProfileARN       string
	RoleARN          string
	RoleName         string
	Certificate      string
	CertificatePath  string
	PrivateKeyPath   string
	CertDir          string
}

func EnsureRolesAnywhere(ctx context.Context, cfg RolesAnywhereConfig, caPEM string, sourceProfile string) (*RolesAnywhereConfig, error) {
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
		existing, listErr := client.ListTrustAnchors(ctx, &rolesanywhere.ListTrustAnchorsInput{})
		if listErr == nil {
			for _, ta := range existing.TrustAnchors {
				if awsroot.ToString(ta.Name) == "GuardianLocalAnchor" {
					result.TrustAnchorARN = awsroot.ToString(ta.TrustAnchorArn)
					break
				}
			}
		}
		if result.TrustAnchorARN == "" {
			return nil, fmt.Errorf("create trust anchor: %w", err)
		}
	}
	if result.TrustAnchorARN == "" && taOut != nil {
		result.TrustAnchorARN = awsroot.ToString(taOut.TrustAnchor.TrustAnchorArn)
	}

	roleARNs := []string{cfg.RoleARN}
	profileOut, err := client.CreateProfile(ctx, &rolesanywhere.CreateProfileInput{
		Name:     awsroot.String("GuardianLocalProfile"),
		RoleArns: roleARNs,
		Enabled:  awsroot.Bool(true),
	})
	if err != nil {
		existing, listErr := client.ListProfiles(ctx, &rolesanywhere.ListProfilesInput{})
		if listErr == nil {
			for _, p := range existing.Profiles {
				if awsroot.ToString(p.Name) == "GuardianLocalProfile" {
					result.ProfileARN = awsroot.ToString(p.ProfileArn)
					break
				}
			}
		}
		if result.ProfileARN == "" {
			return nil, fmt.Errorf("create profile: %w", err)
		}
	}
	if result.ProfileARN == "" && profileOut != nil {
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
credential_process = aws_signing_helper credential-process \\
  --certificate %s \\
  --private-key %s \\
  --trust-anchor-arn %s \\
  --profile-arn %s \\
  --role-arn %s
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
