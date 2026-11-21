// Command audit-anchor builds the daily Merkle manifest for the audit trail and
// publishes it to an S3 bucket with Object Lock COMPLIANCE.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/radryc/guardian/internal/auditanchor"
)

type options struct {
	walPath          string
	date             string
	bucket           string
	prefix           string
	kmsKeyID         string
	retentionDays    int
	prevManifestHash string
	outManifest      string
	outDir           string
	chainFile        string
	chURL            string
	chDatabase       string
	chUser           string
	chPassword       string
	loop             bool
	interval         time.Duration
	backfillDays     int
	healthAddr       string
}

type anchorHealth struct {
	mu          sync.Mutex
	lastSuccess time.Time
	lastError   string
	lastDay     string
}

func (h *anchorHealth) success(day string) {
	h.mu.Lock()
	h.lastSuccess = time.Now().UTC()
	h.lastDay = day
	h.lastError = ""
	h.mu.Unlock()
}

func (h *anchorHealth) failure(err error) {
	h.mu.Lock()
	h.lastError = err.Error()
	h.mu.Unlock()
}

func (h *anchorHealth) snapshot() (time.Time, string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSuccess, h.lastError, h.lastDay
}

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

	var opts options
	fs.StringVar(&opts.walPath, "wal", "", "path to WAL JSONL file (optional when ClickHouse is configured)")
	fs.StringVar(&opts.chURL, "ch-url", envString("AUDIT_CH_URL"), "ClickHouse HTTP URL for audit.events")
	fs.StringVar(&opts.chDatabase, "ch-database", envString("AUDIT_CH_DATABASE"), "ClickHouse audit database (default audit)")
	fs.StringVar(&opts.chUser, "ch-user", envString("AUDIT_CH_USER"), "ClickHouse user")
	fs.StringVar(&opts.chPassword, "ch-password", envString("AUDIT_CH_PASSWORD"), "ClickHouse password")
	fs.StringVar(&opts.date, "date", "", "UTC date to anchor (YYYY-MM-DD; default yesterday in loop mode, today otherwise)")
	fs.StringVar(&opts.bucket, "bucket", "", "S3 bucket for Object Lock COMPLIANCE upload")
	fs.StringVar(&opts.prefix, "prefix", "", "S3 key prefix")
	fs.StringVar(&opts.kmsKeyID, "kms-key-id", "", "AWS KMS key ID or ARN for aws:kms encryption")
	fs.IntVar(&opts.retentionDays, "retention-days", auditanchor.DefaultRetentionDays, "S3 Object Lock COMPLIANCE retention period in days")
	fs.StringVar(&opts.prevManifestHash, "prev-manifest-hash", "", "previous day's manifest hash")
	fs.StringVar(&opts.outManifest, "out-manifest", "", "local file path to write manifest JSON")
	fs.StringVar(&opts.outDir, "out-dir", "", "directory for per-day manifest JSON files (loop/backfill)")
	fs.StringVar(&opts.chainFile, "chain-file", "", "file used to persist the last manifest hash across loop iterations")
	fs.BoolVar(&opts.loop, "loop", false, "run forever, anchoring the previous UTC day")
	fs.DurationVar(&opts.interval, "interval", 24*time.Hour, "loop interval")
	fs.IntVar(&opts.backfillDays, "backfill-days", 0, "on start, anchor this many preceding days (loop only)")
	fs.StringVar(&opts.healthAddr, "health-addr", "", "HTTP listen address for /healthz (for example :8099)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(opts.walPath) == "" && strings.TrimSpace(opts.chURL) == "" {
		return errors.New("--wal or --ch-url is required")
	}
	if strings.TrimSpace(opts.outManifest) == "" && strings.TrimSpace(opts.outDir) == "" && strings.TrimSpace(opts.bucket) == "" {
		return errors.New("at least one of --out-manifest, --out-dir, or --bucket is required")
	}
	if opts.chainFile != "" {
		if raw, err := os.ReadFile(filepath.Clean(opts.chainFile)); err == nil {
			if opts.prevManifestHash == "" {
				opts.prevManifestHash = strings.TrimSpace(string(raw))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read chain file: %w", err)
		}
	}

	health := &anchorHealth{}
	if strings.TrimSpace(opts.healthAddr) != "" {
		go serveAnchorHealth(ctx, opts.healthAddr, health, opts.interval, stderr)
	}

	if !opts.loop {
		day := opts.date
		if strings.TrimSpace(day) == "" {
			day = time.Now().UTC().Format("2006-01-02")
		}
		if err := anchorDay(ctx, &opts, day, stdout); err != nil {
			health.failure(err)
			return err
		}
		health.success(day)
		return nil
	}

	// Backfill oldest-first, then anchor yesterday and sleep.
	for i := opts.backfillDays; i >= 1; i-- {
		day := time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02")
		if err := anchorDay(ctx, &opts, day, stdout); err != nil {
			health.failure(err)
			return err
		}
		health.success(day)
	}
	for {
		day := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		if err := anchorDay(ctx, &opts, day, stdout); err != nil {
			health.failure(err)
			fmt.Fprintf(stderr, "audit-anchor: %v\n", err)
		} else {
			health.success(day)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(opts.interval):
		}
	}
}

func anchorDay(ctx context.Context, opts *options, day string, stdout io.Writer) error {
	var (
		events []auditanchor.EventRecord
		err    error
	)
	if strings.TrimSpace(opts.chURL) != "" {
		events, err = auditanchor.ReadClickHouseDay(ctx, auditanchor.ClickHouseConfig{
			URL:      opts.chURL,
			Database: opts.chDatabase,
			User:     opts.chUser,
			Password: opts.chPassword,
		}, day)
	} else {
		events, err = auditanchor.ReadWALFile(opts.walPath)
	}
	if err != nil {
		return err
	}

	manifest, err := auditanchor.BuildManifest(day, events, opts.prevManifestHash)
	if err != nil {
		return err
	}

	if strings.TrimSpace(opts.outManifest) != "" {
		if err := writeManifest(opts.outManifest, manifest); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote manifest %s (day=%s events=%d merkleRoot=%s manifestHash=%s)\n",
			opts.outManifest, day, manifest.EventCount, manifest.MerkleRoot, manifest.ManifestHash)
	}
	if strings.TrimSpace(opts.outDir) != "" {
		path := filepath.Join(opts.outDir, "manifest-"+day+".json")
		if err := writeManifest(path, manifest); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote manifest %s (day=%s events=%d merkleRoot=%s manifestHash=%s)\n",
			path, day, manifest.EventCount, manifest.MerkleRoot, manifest.ManifestHash)
	}

	if strings.TrimSpace(opts.bucket) != "" {
		uploader, err := auditanchor.NewS3AnchorUploader(ctx, auditanchor.S3AnchorConfig{
			Bucket:        opts.bucket,
			Prefix:        opts.prefix,
			KMSKeyID:      opts.kmsKeyID,
			RetentionDays: opts.retentionDays,
		})
		if err != nil {
			return err
		}
		key, err := uploader.UploadManifest(ctx, manifest)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "uploaded manifest s3://%s/%s (day=%s events=%d merkleRoot=%s manifestHash=%s)\n",
			strings.TrimSpace(opts.bucket), key, day, manifest.EventCount, manifest.MerkleRoot, manifest.ManifestHash)
	}

	if opts.chainFile != "" {
		if err := os.WriteFile(filepath.Clean(opts.chainFile), []byte(manifest.ManifestHash+"\n"), 0o600); err != nil {
			return fmt.Errorf("write chain file: %w", err)
		}
	}
	opts.prevManifestHash = manifest.ManifestHash
	return nil
}

func serveAnchorHealth(ctx context.Context, addr string, health *anchorHealth, interval time.Duration, stderr io.Writer) {
	maxAge := 2 * interval
	if maxAge < 26*time.Hour {
		maxAge = 26 * time.Hour
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		last, lastErr, day := health.snapshot()
		ok := !last.IsZero() && time.Since(last) <= maxAge
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            ok,
			"lastSuccessAt": last.Format(time.RFC3339),
			"lastDay":       day,
			"lastError":     lastErr,
			"maxAge":        maxAge.String(),
		})
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "audit-anchor health server: %v\n", err)
	}
}

func writeManifest(path string, manifest auditanchor.Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	cleanOut := filepath.Clean(path)
	if dir := filepath.Dir(cleanOut); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(cleanOut, data, 0o600); err != nil {
		return fmt.Errorf("write manifest file: %w", err)
	}
	return nil
}

func envString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
