package command

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunGroupHelpPrintsSubcommands(t *testing.T) {
	reg := New()
	reg.Register("rollout", "tag", &Command{
		Description: "Tag asset versions",
		Flags:       flag.NewFlagSet("rollout tag", flag.ContinueOnError),
	})

	output := captureStderr(t, func() {
		if err := reg.Run(context.Background(), []string{"rollout", "--help"}); err != nil {
			t.Fatalf("Run(group --help) error = %v", err)
		}
	})

	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected usage output, got %q", output)
	}
	if !strings.Contains(output, "rollout <command>") {
		t.Fatalf("expected group usage, got %q", output)
	}
	if !strings.Contains(output, "tag") || !strings.Contains(output, "Tag asset versions") {
		t.Fatalf("expected group command list, got %q", output)
	}
}

func TestRunHelpTopicPrintsCommandUsage(t *testing.T) {
	reg := New()
	flags := flag.NewFlagSet("rollout tag", flag.ContinueOnError)
	flags.String("dir", "", "local partition directory")
	reg.Register("rollout", "tag", &Command{
		Description: "Tag asset versions",
		Flags:       flags,
	})

	output := captureStderr(t, func() {
		if err := reg.Run(context.Background(), []string{"help", "rollout", "tag"}); err != nil {
			t.Fatalf("Run(help rollout tag) error = %v", err)
		}
	})

	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected usage output, got %q", output)
	}
	if !strings.Contains(output, "rollout tag") {
		t.Fatalf("expected command usage, got %q", output)
	}
	if !strings.Contains(output, "-dir") {
		t.Fatalf("expected flags in command usage, got %q", output)
	}
	if !strings.Contains(output, "Extended help:") || !strings.Contains(output, "Detailed flags:") {
		t.Fatalf("expected extended help output, got %q", output)
	}
}

func TestRunCommandExtendedHelpPrintsDetailedFlags(t *testing.T) {
	reg := New()
	flags := flag.NewFlagSet("rollout tag", flag.ContinueOnError)
	flags.String("dir", "", "local partition directory")
	flags.Bool("force", false, "overwrite existing version tags")
	reg.Register("rollout", "tag", &Command{
		Description: "Tag asset versions",
		Flags:       flags,
	})

	output := captureStderr(t, func() {
		if err := reg.Run(context.Background(), []string{"rollout", "tag", "--help-full"}); err != nil {
			t.Fatalf("Run(rollout tag --help-full) error = %v", err)
		}
	})

	if !strings.Contains(output, "Extended help:") {
		t.Fatalf("expected extended help output, got %q", output)
	}
	if !strings.Contains(output, "--dir <string>") {
		t.Fatalf("expected typed string flag in extended help, got %q", output)
	}
	if !strings.Contains(output, "--force") || !strings.Contains(output, "type: bool") {
		t.Fatalf("expected bool flag details in extended help, got %q", output)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer reader.Close()
	original := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = original }()

	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		outputCh <- string(data)
	}()

	fn()

	_ = writer.Close()
	return <-outputCh
}
