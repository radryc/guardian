package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radryc/guardian/internal/auditanchor"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
)

func writeAuditFixture(t *testing.T, prevManifestHash string) (manifestPath, walPath string, events []auditanchor.EventRecord, manifest auditanchor.Manifest) {
	t.Helper()
	base := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	raw := []auditanchor.EventRecord{
		{
			EventID:   "evt-1",
			Sequence:  1,
			Timestamp: base,
			Actor:     "alice@example.com",
			Action:    "partition.push",
			Partition: "prod",
			Outcome:   "success",
		},
		{
			EventID:   "evt-2",
			Sequence:  2,
			Timestamp: base.Add(time.Minute),
			Actor:     "bob@example.com",
			Action:    "access.publish",
			Partition: "prod",
			Outcome:   "success",
		},
	}
	var err error
	events, err = auditanchor.ChainEvents(raw, "")
	if err != nil {
		t.Fatalf("ChainEvents() error = %v", err)
	}
	manifest, err = auditanchor.BuildManifest("2026-11-18", events, prevManifestHash)
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	dir := t.TempDir()
	manifestPath = filepath.Join(dir, "manifest.json")
	walPath = filepath.Join(dir, "events.wal.jsonl")

	mBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	if err := os.WriteFile(manifestPath, mBytes, 0o600); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	var walBuf strings.Builder
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("Marshal(event) error = %v", err)
		}
		walBuf.Write(b)
		walBuf.WriteByte('\n')
	}
	if err := os.WriteFile(walPath, []byte(walBuf.String()), 0o600); err != nil {
		t.Fatalf("WriteFile(wal) error = %v", err)
	}
	return manifestPath, walPath, events, manifest
}

func TestGuardianctlAuditVerifyOfflineSuccess(t *testing.T) {
	prevHash := strings.Repeat("a", 64)
	manifestPath, walPath, _, manifest := writeAuditFixture(t, prevHash)

	var out bytes.Buffer
	printer := &output.Printer{Format: cliformat.FormatText, Writer: &out}
	reg := registerCommands(nil, printer)

	err := reg.Run(context.Background(), []string{
		"audit", "verify",
		"--manifest", manifestPath,
		"--events", walPath,
		"--prev-manifest-hash", prevHash,
	})
	if err != nil {
		t.Fatalf("audit verify failed: %v", err)
	}
	if !strings.Contains(out.String(), "verified 2 events") {
		t.Fatalf("unexpected output: %q", out.String())
	}

	// JSON output mode
	var jsonOut bytes.Buffer
	jsonPrinter := &output.Printer{Format: cliformat.FormatJSON, Writer: &jsonOut}
	jsonReg := registerCommands(nil, jsonPrinter)
	if err := jsonReg.Run(context.Background(), []string{
		"audit", "verify",
		"--manifest", manifestPath,
		"--events", walPath,
	}); err != nil {
		t.Fatalf("audit verify json failed: %v", err)
	}
	var res auditVerifyResult
	if err := json.Unmarshal(jsonOut.Bytes(), &res); err != nil {
		t.Fatalf("decode json output: %v (raw=%q)", err, jsonOut.String())
	}
	if !res.Verified || res.EventCount != 2 || res.ManifestHash != manifest.ManifestHash {
		t.Fatalf("unexpected json result: %+v", res)
	}
}

func TestGuardianctlAuditVerifyDetectsTampering(t *testing.T) {
	prevHash := strings.Repeat("b", 64)
	manifestPath, walPath, events, _ := writeAuditFixture(t, prevHash)

	t.Run("tampered WAL event", func(t *testing.T) {
		tamperedWAL := filepath.Join(t.TempDir(), "tampered.wal.jsonl")
		events[1].Actor = "attacker@example.com"
		var walBuf strings.Builder
		for _, ev := range events {
			b, _ := json.Marshal(ev)
			walBuf.Write(b)
			walBuf.WriteByte('\n')
		}
		if err := os.WriteFile(tamperedWAL, []byte(walBuf.String()), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		reg := registerCommands(nil, &output.Printer{Format: cliformat.FormatText, Writer: &bytes.Buffer{}})
		err := reg.Run(context.Background(), []string{
			"audit", "verify",
			"--manifest", manifestPath,
			"--events", tamperedWAL,
		})
		if err == nil {
			t.Fatal("expected error for tampered WAL event, got nil")
		}
	})

	t.Run("mismatched prev-manifest-hash", func(t *testing.T) {
		reg := registerCommands(nil, &output.Printer{Format: cliformat.FormatText, Writer: &bytes.Buffer{}})
		err := reg.Run(context.Background(), []string{
			"audit", "verify",
			"--manifest", manifestPath,
			"--events", walPath,
			"--prev-manifest-hash", strings.Repeat("f", 64),
		})
		if err == nil {
			t.Fatal("expected error for mismatched --prev-manifest-hash, got nil")
		}
	})

	t.Run("missing required flags", func(t *testing.T) {
		reg := registerCommands(nil, &output.Printer{Format: cliformat.FormatText, Writer: &bytes.Buffer{}})
		if err := reg.Run(context.Background(), []string{"audit", "verify"}); err == nil {
			t.Fatal("expected error for missing flags, got nil")
		}
	})
}
