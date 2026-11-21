package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radryc/guardian/internal/auditanchor"
	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
)

type auditVerifyResult struct {
	Verified         bool   `json:"verified"`
	Date             string `json:"date"`
	EventCount       int    `json:"eventCount"`
	MerkleRoot       string `json:"merkleRoot"`
	ManifestHash     string `json:"manifestHash"`
	PrevManifestHash string `json:"prevManifestHash,omitempty"`
}

// auditVerifyCommand verifies an anchored audit manifest against a WAL JSONL file offline.
func auditVerifyCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "", "path to anchored manifest JSON file (required)")
	eventsPath := flags.String("events", "", "path to WAL JSONL events file (required)")
	prevManifestHash := flags.String("prev-manifest-hash", "", "expected previous manifest hash (optional)")

	return &command.Command{
		Description: "Verify an audit manifest and WAL event chain offline",
		Flags:       flags,
		Run: func(_ context.Context, _ []string) error {
			if strings.TrimSpace(*manifestPath) == "" {
				return fmt.Errorf("--manifest is required")
			}
			if strings.TrimSpace(*eventsPath) == "" {
				return fmt.Errorf("--events is required")
			}

			manifestBytes, err := os.ReadFile(filepath.Clean(*manifestPath))
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			var manifest auditanchor.Manifest
			if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
				return fmt.Errorf("decode manifest: %w", err)
			}

			events, err := auditanchor.ReadWALFile(*eventsPath)
			if err != nil {
				return err
			}

			if err := auditanchor.VerifyBundle(manifest, events, *prevManifestHash); err != nil {
				return fmt.Errorf("audit verification failed: %w", err)
			}

			if printer != nil && printer.Format == cliformat.FormatJSON {
				printer.PrintJSON(auditVerifyResult{
					Verified:         true,
					Date:             manifest.Date,
					EventCount:       manifest.EventCount,
					MerkleRoot:       manifest.MerkleRoot,
					ManifestHash:     manifest.ManifestHash,
					PrevManifestHash: manifest.PrevManifestHash,
				})
				return nil
			}
			if printer != nil {
				printer.PrintText("verified %d events for %s (merkleRoot=%s manifestHash=%s)\n",
					manifest.EventCount, manifest.Date, manifest.MerkleRoot, manifest.ManifestHash)
			}
			return nil
		},
	}
}
