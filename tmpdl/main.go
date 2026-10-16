package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
)

func main() {
	ctx := context.Background()
	store, closer, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
		RouterAddr: os.Args[1], Token: os.Args[2], PrincipalID: "guardianctl",
		Role: "cli", ClientIDPrefix: "guardian-cli", Version: "guardianctl",
		Writable: true, UseExternalAddresses: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer closer.Close()
	logicalDir, outDir := os.Args[3], os.Args[4]
	root := strings.TrimRight(logicalDir, "/")
	files := 0
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := store.ListDir(ctx, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := strings.TrimRight(dir, "/") + "/" + e.Name
			if e.IsDir {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			data, err := store.ReadFile(ctx, p)
			if err != nil {
				return err
			}
			rel := strings.TrimPrefix(p, root+"/")
			target := filepath.Join(outDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0o644); err != nil {
				return err
			}
			files++
		}
		return nil
	}
	if err := walk(logicalDir); err != nil {
		fmt.Fprintln(os.Stderr, "walk:", err)
		os.Exit(1)
	}
	fmt.Printf("dumped %d files to %s\n", files, outDir)
}
