package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/schema"
)

func schemaCommand() *command.Command {
	flags := flag.NewFlagSet("schema", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	out := flags.String("out", "schemas", "directory to write schema documents")
	stdout := flags.Bool("stdout", false, "write partition.schema.json to stdout instead of files")
	return &command.Command{
		Description: "Generate JSON Schema documents for partitions, intents, and the asset catalog",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			docs, err := schema.Documents()
			if err != nil {
				return err
			}
			if *stdout {
				_, err := os.Stdout.Write(docs[schema.PartitionFile])
				return err
			}
			if err := os.MkdirAll(*out, 0o755); err != nil {
				return err
			}
			names := make([]string, 0, len(docs))
			for name := range docs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				path := filepath.Join(*out, name)
				if err := os.WriteFile(path, docs[name], 0o644); err != nil {
					return err
				}
				fmt.Printf("wrote %s\n", path)
			}
			return nil
		},
	}
}
