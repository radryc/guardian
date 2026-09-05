package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"gopkg.in/yaml.v3"
)

type partitionValidateLocalResult struct {
	Success           bool     `json:"success"`
	Root              string   `json:"root"`
	IncludeSecrets    bool     `json:"includeSecrets"`
	PartitionsChecked int      `json:"partitionsChecked"`
	YAMLFilesParsed   int      `json:"yamlFilesParsed"`
	Errors            []string `json:"errors,omitempty"`
}

func partitionValidateLocalCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition validate-local", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	root := flags.String("root", "./partitions", "root directory containing partition subdirectories")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/ when validating partition bundles")

	return &command.Command{Description: "Validate local partition manifests and parse all partition YAML files", Flags: flags, Run: func(ctx context.Context, args []string) error {
		_ = ctx
		result, err := validateLocalPartitionsRoot(filepath.Clean(*root), *includeSecrets)
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(result)
		} else {
			printer.PrintText(
				"checked %d partition(s), parsed %d YAML file(s) under %s\n",
				result.PartitionsChecked,
				result.YAMLFilesParsed,
				result.Root,
			)
			if len(result.Errors) == 0 {
				printer.PrintText("local partition validation passed\n")
			} else {
				printer.PrintText("local partition validation found %d error(s):\n", len(result.Errors))
				for _, item := range result.Errors {
					printer.PrintText("- %s\n", item)
				}
			}
		}
		if err != nil {
			return err
		}
		return nil
	}}
}

func validateLocalPartitionsRoot(root string, includeSecrets bool) (partitionValidateLocalResult, error) {
	result := partitionValidateLocalResult{
		Success:        true,
		Root:           root,
		IncludeSecrets: includeSecrets,
	}

	partitionDirs, err := discoverLocalPartitionDirs(root)
	if err != nil {
		result.Success = false
		result.Errors = []string{err.Error()}
		return result, err
	}
	result.PartitionsChecked = len(partitionDirs)

	for _, dir := range partitionDirs {
		relDir := dir
		if rel, relErr := filepath.Rel(root, dir); relErr == nil {
			relDir = filepath.ToSlash(rel)
		}

		if _, loadErr := loadLocalPartitionBundle(dir, includeSecrets); loadErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relDir, loadErr))
		}

		count, parseErrs := parseAllYAMLFilesInDir(dir)
		result.YAMLFilesParsed += count
		for _, parseErr := range parseErrs {
			relPath := parseErr.path
			if rel, relErr := filepath.Rel(root, parseErr.path); relErr == nil {
				relPath = filepath.ToSlash(rel)
			}
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relPath, parseErr.err))
		}
	}

	if len(result.Errors) == 0 {
		return result, nil
	}
	result.Success = false
	return result, fmt.Errorf("local partition validation failed with %d error(s)", len(result.Errors))
}

func discoverLocalPartitionDirs(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", root)
	}

	if _, err := os.Stat(filepath.Join(root, "config.yaml")); err == nil {
		return []string{root}, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", filepath.Join(root, "config.yaml"), err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read root directory %s: %w", root, err)
	}
	partitionDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
			partitionDirs = append(partitionDirs, dir)
		}
	}
	sort.Strings(partitionDirs)
	if len(partitionDirs) == 0 {
		return nil, fmt.Errorf("no partition directories with config.yaml found under %s", root)
	}
	return partitionDirs, nil
}

type yamlParseError struct {
	path string
	err  error
}

func parseAllYAMLFilesInDir(dir string) (int, []yamlParseError) {
	parsed := 0
	problems := make([]yamlParseError, 0)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			problems = append(problems, yamlParseError{path: path, err: walkErr})
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".state" {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		parsed++
		content, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, yamlParseError{path: path, err: err})
			return nil
		}
		if err := parseYAMLDocuments(content); err != nil {
			problems = append(problems, yamlParseError{path: path, err: err})
		}
		return nil
	})
	return parsed, problems
}

func parseYAMLDocuments(raw []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var node any
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
