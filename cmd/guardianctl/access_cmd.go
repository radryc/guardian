package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/authz/policy"
	"github.com/radryc/guardian/internal/access"
	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// loadRawPolicy reads a raw ACL from disk and resolves its imports relative to
// the file. It returns the merged file plus any import warnings.
func loadRawPolicy(filePath string) (*policy.File, []string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Dir(filePath)
	read := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, rel))
	}
	return policy.LoadFile(data, filePath, read)
}

// loadCatalogFile reads a partition catalog snapshot ("{partitions:[{name,labels}]}").
func loadCatalogFile(filePath string) (*policy.Catalog, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var catalog policy.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse catalog file: %w", err)
	}
	return &catalog, nil
}

// loadCompiledPolicy reads a compiled bundle from disk.
func loadCompiledPolicy(filePath string) (*policy.Policy, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return policy.ParseBundle(data)
}

// resolvePolicy loads either a compiled bundle (--file) or a raw ACL (--raw,
// optionally with --catalog-file) for read-only commands.
func resolvePolicy(compiledFile, rawFile, catalogFile string) (*policy.Policy, []policy.Issue, error) {
	compiledFile = strings.TrimSpace(compiledFile)
	rawFile = strings.TrimSpace(rawFile)
	switch {
	case compiledFile != "" && rawFile != "":
		return nil, nil, fmt.Errorf("use either --file (compiled bundle) or --raw (raw ACL), not both")
	case compiledFile != "":
		p, err := loadCompiledPolicy(compiledFile)
		return p, nil, err
	case rawFile != "":
		file, warnings, err := loadRawPolicy(rawFile)
		if err != nil {
			return nil, nil, err
		}
		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
		catalog, err := loadCatalogFile(catalogFile)
		if err != nil {
			return nil, nil, err
		}
		bundle, issues, err := policy.Compile(file, catalog, time.Now())
		if err != nil {
			return nil, issues, err
		}
		return policy.New(*bundle), issues, nil
	default:
		return nil, nil, fmt.Errorf("one of --file <compiled.json> or --raw <access.yaml> is required")
	}
}

func printIssues(printer *output.Printer, issues []policy.Issue) int {
	errors := 0
	for _, issue := range issues {
		printer.PrintText("%s: %s\n", issue.Severity, issue.Message)
		if issue.Severity == "error" {
			errors++
		}
	}
	return errors
}

// accessLintCommand validates a raw ACL (imports resolved from disk) and
// reports findings. It exits non-zero when any error-severity issue exists.
func accessLintCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("access lint", flag.ContinueOnError)
	file := flags.String("file", "", "raw ACL file (access.yaml), required")
	catalogFile := flags.String("catalog-file", "", "optional partition catalog JSON: {\"partitions\":[{\"name\":\"x\",\"labels\":{...}}]}")
	return &command.Command{Description: "Validate a raw access policy", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if strings.TrimSpace(*file) == "" {
			return fmt.Errorf("--file is required")
		}
		parsed, warnings, err := loadRawPolicy(*file)
		if err != nil {
			return err
		}
		for _, warning := range warnings {
			printer.PrintText("warning: %s\n", warning)
		}
		catalog, err := loadCatalogFile(*catalogFile)
		if err != nil {
			return err
		}
		issues := policy.Lint(parsed, catalog)
		if printer.Format == "json" {
			printer.PrintJSON(issues)
		} else if errors := printIssues(printer, issues); errors > 0 {
			return fmt.Errorf("%d error(s)", errors)
		} else {
			printer.PrintText("ok: no errors (%d issue(s))\n", len(issues))
		}
		for _, issue := range issues {
			if issue.Severity == "error" {
				return fmt.Errorf("lint failed")
			}
		}
		return nil
	}}
}

// accessCompileCommand compiles a raw ACL into the versioned bundle consumed
// by Guardian, MonoFS and Doctor.
func accessCompileCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("access compile", flag.ContinueOnError)
	file := flags.String("file", "", "raw ACL file (access.yaml), required")
	out := flags.String("out", "", "output path for the compiled bundle JSON, required")
	catalogFile := flags.String("catalog-file", "", "optional partition catalog JSON (selectors/globs resolve against it)")
	return &command.Command{Description: "Compile a raw access policy into the versioned bundle", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if strings.TrimSpace(*file) == "" || strings.TrimSpace(*out) == "" {
			return fmt.Errorf("--file and --out are required")
		}
		parsed, warnings, err := loadRawPolicy(*file)
		if err != nil {
			return err
		}
		for _, warning := range warnings {
			printer.PrintText("warning: %s\n", warning)
		}
		catalog, err := loadCatalogFile(*catalogFile)
		if err != nil {
			return err
		}
		bundle, issues, err := policy.Compile(parsed, catalog, time.Now())
		printIssues(printer, issues)
		if err != nil {
			return err
		}
		data, err := policy.MarshalBundle(*bundle)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return err
		}
		printer.PrintText("compiled %s (version %s, %d grant(s))\n", *out, bundle.Version, len(bundle.Grants))
		return nil
	}}
}

// accessExplainCommand reports the grants matching a user.
func accessExplainCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("access explain", flag.ContinueOnError)
	user := flags.String("user", "", "user id (email or OIDC subject), required")
	groups := flags.String("groups", "", "comma-separated IdP groups the user belongs to")
	file := flags.String("file", "", "compiled policy bundle JSON")
	raw := flags.String("raw", "", "raw ACL file (alternative to --file)")
	catalogFile := flags.String("catalog-file", "", "partition catalog JSON for --raw")
	return &command.Command{Description: "Explain the access a user has", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if strings.TrimSpace(*user) == "" {
			return fmt.Errorf("--user is required")
		}
		p, issues, err := resolvePolicy(*file, *raw, *catalogFile)
		if err != nil {
			return err
		}
		printIssues(printer, issues)
		id := authz.Identity{Subject: strings.TrimSpace(*user)}
		if strings.Contains(id.Subject, "@") {
			id.Email = id.Subject
		}
		for _, group := range strings.Split(*groups, ",") {
			if group = strings.TrimSpace(group); group != "" {
				id.Groups = append(id.Groups, group)
			}
		}
		explanation := p.Explain(id)
		if printer.Format == "json" {
			printer.PrintJSON(explanation)
			return nil
		}
		printer.PrintText("admin: %v\n", explanation.Admin)
		printer.PrintText("defaults: partitions=%v logs=%v traces=%v monofs=%v\n",
			explanation.Defaults.Partitions, explanation.Defaults.Logs,
			explanation.Defaults.Traces, explanation.Defaults.MonoFS)
		rows := make([][]string, 0, len(explanation.Matches))
		for _, grant := range explanation.Matches {
			rows = append(rows, []string{
				grantSubject(grant),
				grant.Role,
				strings.Join(grant.Partitions, ","),
				strings.Join(grant.Logs, ","),
				strings.Join(grant.Traces, ","),
				strings.Join(grant.MonoFS, ","),
				strings.Join(grant.Deny, ","),
			})
		}
		printer.PrintText("matches: %d\n", len(rows))
		printer.PrintTable([]string{"SUBJECT", "ROLE", "PARTITIONS", "LOGS", "TRACES", "MONOFS", "DENY"}, rows)
		return nil
	}}
}

// accessWhoCanCommand lists subjects with access to a scope (optionally one
// partition).
func accessWhoCanCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("access who-can", flag.ContinueOnError)
	scope := flags.String("scope", "partitions", "scope to inspect: partitions, logs, traces or monofs")
	partition := flags.String("partition", "", "restrict to one partition name")
	file := flags.String("file", "", "compiled policy bundle JSON")
	raw := flags.String("raw", "", "raw ACL file (alternative to --file)")
	catalogFile := flags.String("catalog-file", "", "partition catalog JSON for --raw")
	return &command.Command{Description: "List subjects allowed on a scope or partition", Flags: flags, Run: func(_ context.Context, _ []string) error {
		p, issues, err := resolvePolicy(*file, *raw, *catalogFile)
		if err != nil {
			return err
		}
		printIssues(printer, issues)
		scopeName := strings.TrimSpace(*scope)
		partitionName := strings.TrimSpace(*partition)
		rows := make([][]string, 0, 8)
		for _, grant := range p.Bundle().Grants {
			var list []string
			switch scopeName {
			case "partitions":
				list = grant.Partitions
			case "logs":
				list = grant.Logs
			case "traces":
				list = grant.Traces
			case "monofs":
				list = grant.MonoFS
			default:
				return fmt.Errorf("unknown scope %q (partitions, logs, traces or monofs)", scopeName)
			}
			if len(list) == 0 {
				continue
			}
			if partitionName != "" && !scopeContains(list, partitionName) {
				continue
			}
			rows = append(rows, []string{grantSubject(grant), grant.Role, strings.Join(list, ",")})
		}
		if printer.Format == "json" {
			printer.PrintJSON(rows)
			return nil
		}
		header := scopeName
		if partitionName != "" {
			header = scopeName + ":" + partitionName
		}
		printer.PrintText("%s: %d subject(s)\n", header, len(rows))
		printer.PrintTable([]string{"SUBJECT", "ROLE", "SCOPE"}, rows)
		return nil
	}}
}

// accessPublishCommand copies a raw ACL (and its imports) into the store and
// writes the compiled bundle next to it, which guardiand picks up and serves.
func accessPublishCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("access publish", flag.ContinueOnError)
	file := flags.String("file", "", "raw ACL file to publish, required")
	env := flags.String("env", "dev", "environment name (store path /.access/envs/<env>.yaml)")
	return &command.Command{Description: "Publish a raw ACL into the store and compile it", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		if strings.TrimSpace(*file) == "" {
			return fmt.Errorf("--file is required")
		}
		envName := strings.TrimSpace(*env)
		if envName == "" {
			return fmt.Errorf("--env is required")
		}

		// Collect the file and its imports, mapping them into the store so
		// guardiand can resolve imports relative to the store path.
		type write struct {
			local string
			store string
		}
		var writes []write
		seen := map[string]bool{}
		var collect func(localPath, storePath string) error
		collect = func(localPath, storePath string) error {
			if seen[storePath] {
				return nil
			}
			seen[storePath] = true
			data, err := os.ReadFile(localPath)
			if err != nil {
				return err
			}
			writes = append(writes, write{local: localPath, store: storePath})
			parsed, err := policy.ParseFile(data)
			if err != nil {
				return fmt.Errorf("%s: %w", localPath, err)
			}
			for _, imp := range parsed.Imports {
				if err := collect(path.Join(filepath.Dir(localPath), imp), path.Join(path.Dir(storePath), imp)); err != nil {
					return err
				}
			}
			return nil
		}
		if err := collect(*file, paths.AccessPolicyPath(envName)); err != nil {
			return err
		}

		parsed, warnings, err := loadRawPolicy(*file)
		if err != nil {
			return err
		}
		for _, warning := range warnings {
			printer.PrintText("warning: %s\n", warning)
		}
		catalog, err := access.LoadCatalog(ctx, store)
		if err != nil {
			return fmt.Errorf("load partition catalog: %w", err)
		}
		bundle, issues, err := policy.Compile(parsed, catalog, time.Now())
		printIssues(printer, issues)
		if err != nil {
			return err
		}
		compiled, err := policy.MarshalBundle(*bundle)
		if err != nil {
			return err
		}

		pathWrites := make([]guardianapi.PathWrite, 0, len(writes)+1)
		for _, w := range writes {
			data, err := os.ReadFile(w.local)
			if err != nil {
				return err
			}
			pathWrites = append(pathWrites, guardianapi.PathWrite{LogicalPath: w.store, Content: data})
		}
		pathWrites = append(pathWrites, guardianapi.PathWrite{
			LogicalPath: paths.AccessCompiledPath(),
			Content:     compiled,
		})
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: pathWrites,
			Context: guardianapi.MutationContext{
				PrincipalID: "guardianctl",
				Reason:      "access publish",
			},
		}); err != nil {
			return err
		}
		printer.PrintText("published env=%s raw=%s compiled=%s version=%s\n",
			envName, paths.AccessPolicyPath(envName), paths.AccessCompiledPath(), bundle.Version)
		return nil
	}}
}

func grantSubject(grant policy.Grant) string {
	switch {
	case grant.Group != "":
		return "group:" + grant.Group
	case grant.User != "":
		return "user:" + grant.User
	case grant.Client != "":
		return "client:" + grant.Client
	}
	return "?"
}

func scopeContains(list []string, name string) bool {
	for _, item := range list {
		if item == "*" || item == name {
			return true
		}
	}
	return false
}
