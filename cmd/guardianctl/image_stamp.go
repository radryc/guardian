package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"gopkg.in/yaml.v3"
)

func imageStampCommand() *command.Command {
	flags := flag.NewFlagSet("image stamp", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dirFlag := flags.String("dir", "", "path to partition directory")
	dryRun := flags.Bool("dry-run", false, "print changes without writing files")

	return &command.Command{
		Description: "Stamp built image refs into partition intent manifests",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			if *dirFlag == "" {
				return fmt.Errorf("--dir is required")
			}
			dir := filepath.Clean(*dirFlag)

			state, err := loadImageState(dir)
			if err != nil {
				return fmt.Errorf("load image state: %w (did you run 'guardianctl image push' first?)", err)
			}

			intentsDir := filepath.Join(dir, "intents")
			entries, err := os.ReadDir(intentsDir)
			if err != nil {
				return fmt.Errorf("read intents directory: %w", err)
			}

			// Pass 1: stamp ImageBuild asset versions and sourceImage
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
					continue
				}
				intentPath := filepath.Join(intentsDir, entry.Name())
				content, err := os.ReadFile(intentPath)
				if err != nil {
					return fmt.Errorf("read intent file %s: %w", intentPath, err)
				}

				var doc yaml.Node
				if err := yaml.Unmarshal(content, &doc); err != nil {
					return fmt.Errorf("parse intent %s: %w", intentPath, err)
				}
				if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
					continue
				}

				modified := false
				assetsNode := findNode(doc.Content[0], "spec", "assets")
				if assetsNode == nil || assetsNode.Kind != yaml.SequenceNode {
					continue
				}

				for _, assetNode := range assetsNode.Content {
					typeNode := findNode(assetNode, "type")
					if typeNode == nil || typeNode.Value != "ImageBuild" {
						continue
					}

					nameNode := findNode(assetNode, "name")
					if nameNode == nil {
						continue
					}
					assetName := nameNode.Value

					imgEntry, ok := state.Images[assetName]
					if !ok || imgEntry.Tag == "" {
						continue
					}

					propsNode := findNode(assetNode, "properties")
					if propsNode == nil || propsNode.Kind != yaml.MappingNode {
						continue
					}

					versionNode := findNode(assetNode, "version")
					if versionNode != nil {
						oldVersion := versionNode.Value
						versionNode.Value = imgEntry.Tag
						fmt.Fprintf(os.Stderr, "  %s/%s: version %s -> %s\n", filepath.Base(intentPath), assetName, oldVersion, imgEntry.Tag)
					}
					modified = true

					if imgEntry.ImageRef != "" {
						setStringNode(propsNode, "sourceImage", imgEntry.ImageRef)
						fmt.Fprintf(os.Stderr, "  %s/%s: sourceImage=%s\n", filepath.Base(intentPath), assetName, imgEntry.ImageRef)
					}
				}

				if modified && !*dryRun {
					out, err := yaml.Marshal(&doc)
					if err != nil {
						return fmt.Errorf("marshal intent %s: %w", intentPath, err)
					}
					if err := os.WriteFile(intentPath, out, 0644); err != nil {
						return fmt.Errorf("write intent %s: %w", intentPath, err)
					}
				}
			}

			// Pass 2: cross-intent ${intent.NAME.outputs.ASSET.FIELD} refs are
			// resolved in-memory at partition push time, not written back to disk,
			// so that template refs are preserved for future builds.
			fmt.Fprintf(os.Stderr, "Resolving cross-intent output references...\n")

			if *dryRun {
				fmt.Fprintln(os.Stderr, "dry-run: no files modified")
			}
			return nil
		},
	}
}
