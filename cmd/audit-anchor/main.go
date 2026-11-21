package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/radryc/guardian/internal/auditanchor"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("audit-anchor", flag.ContinueOnError)
	fs.SetOutput(stderr)

	walPath := fs.String("wal", "", "path to WAL JSONL file (required)")
	date := fs.String("date", time.Now().UTC().Format("2006-01-02"), "UTC date for the manifest (YYYY-MM-DD)")
	bucket := fs.String("bucket", "", "S3 bucket for Object Lock COMPLIANCE upload")
	prefix := fs.String("prefix", "", "S3 key prefix")
	kmsKeyID := fs.String("kms-key-id", "", "AWS KMS key ID or ARN for aws:kms encryption")
	retentionDays := fs.Int("retention-days", auditanchor.DefaultRetentionDays, "S3 Object Lock COMPLIANCE retention period in days")
	prevManifestHash := fs.String("prev-manifest-hash", "", "previous day's manifest hash")
	outManifest := fs.String("out-manifest", "", "local file path to write manifest JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if strings.TrimSpace(*walPath) == "" {
		return errors.New("--wal is required")
	}
	if strings.TrimSpace(*outManifest) == "" && strings.TrimSpace(*bucket) == "" {
		return errors.New("at least one of --out-manifest or --bucket is required")
	}

	events, err := auditanchor.ReadWALFile(*walPath)
	if err != nil {
		return err
	}

	manifest, err := auditanchor.BuildManifest(*date, events, *prevManifestHash)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*outManifest) != "" {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
		data = append(data, '\n')
		cleanOut := filepath.Clean(*outManifest)
		if dir := filepath.Dir(cleanOut); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
		}
		if err := os.WriteFile(cleanOut, data, 0o600); err != nil {
			return fmt.Errorf("write manifest file: %w", err)
		}
		fmt.Fprintf(stdout, "wrote manifest %s (events=%d merkleRoot=%s manifestHash=%s)\n",
			cleanOut, manifest.EventCount, manifest.MerkleRoot, manifest.ManifestHash)
	}

	if strings.TrimSpace(*bucket) != "" {
		uploader, err := auditanchor.NewS3AnchorUploader(ctx, auditanchor.S3AnchorConfig{
			Bucket:        *bucket,
			Prefix:        *prefix,
			KMSKeyID:      *kmsKeyID,
			RetentionDays: *retentionDays,
		})
		if err != nil {
			return err
		}
		key, err := uploader.UploadManifest(ctx, manifest)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "uploaded manifest s3://%s/%s (events=%d merkleRoot=%s manifestHash=%s)\n",
			strings.TrimSpace(*bucket), key, manifest.EventCount, manifest.MerkleRoot, manifest.ManifestHash)
	}

	return nil
}
