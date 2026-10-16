package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cdkgen"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// templateListCommand lists the effective template catalog (embedded baseline
// plus store overlays) so operators can see which template renders an asset
// type before a reconcile.
func templateListCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("template list", flag.ContinueOnError)
	partition := flags.String("partition", "", "include partition-scoped template overlays")
	assetType := flags.String("type", "", "filter by asset type")
	return &command.Command{Description: "List the effective CDK template catalog (embedded + store overlays)", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		provider := cdkgen.NewOverlayProvider(ctx, store, strings.TrimSpace(*partition), nil)
		catalog := provider.Catalog()
		for _, warning := range provider.Warnings() {
			printer.PrintText("warning: %s\n", warning)
		}
		filter := strings.TrimSpace(*assetType)
		rows := make([][]string, 0, len(catalog.Templates))
		for _, entry := range catalog.Templates {
			if filter != "" && !containsFold(entry.AssetTypes, filter) {
				continue
			}
			types := strings.Join(entry.AssetTypes, ",")
			if entry.Implicit {
				types = "(implicit)"
			}
			fallback := ""
			if entry.Fallback {
				fallback = "fallback"
			}
			rows = append(rows, []string{
				entry.ID,
				types,
				fmt.Sprint(entry.Version),
				fmt.Sprint(entry.Priority),
				fallback,
				entry.Module,
			})
		}
		if printer.Format == "json" {
			printer.PrintJSON(catalog.Templates)
			return nil
		}
		printer.PrintText("catalog %s (%d template(s))\n", catalog.Version, len(rows))
		printer.PrintTable([]string{"ID", "ASSET TYPES", "VERSION", "PRIORITY", "FLAGS", "MODULE"}, rows)
		return nil
	}}
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
