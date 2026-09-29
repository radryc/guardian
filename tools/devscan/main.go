package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: devscan <write|read|list> [flags]")
	}
	mode := os.Args[1]
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	router := fs.String("monofs-router", "127.0.0.1:9090", "MonoFS router gRPC address")
	token := fs.String("monofs-token", "", "MonoFS guardian token")
	pusher := fs.String("pusher", "", "AWS pusher name (e.g. aws-975049940689)")
	account := fs.String("account", "", "AWS account to scan")
	region := fs.String("region", "us-east-1", "AWS region to scan")
	scanID := fs.String("scan-id", "", "scan id (read/write)")
	external := fs.Bool("monofs-use-external-addresses", false, "use router-advertised external node addresses")
	_ = fs.Parse(os.Args[2:])

	if *pusher == "" {
		log.Fatal("--pusher is required")
	}

	ctx := context.Background()
	store, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
		RouterAddr:           *router,
		Token:                *token,
		PrincipalID:          "devscan",
		Role:                 "cli",
		ClientIDPrefix:       "guardian-devscan",
		Version:              "devscan",
		UseExternalAddresses: *external,
		Writable:             true,
	})
	if err != nil {
		log.Fatalf("open monofs store: %v", err)
	}
	defer func() { _ = client.Close() }()

	switch mode {
	case "write":
		if *account == "" {
			log.Fatal("--account is required")
		}
		if *scanID == "" {
			*scanID = fmt.Sprintf("scan-%d", time.Now().Unix())
		}
		req := awsscan.ScanRequest{
			APIVersion:      awsscan.APIVersion,
			Kind:            awsscan.RequestKind,
			ScanID:          *scanID,
			Account:         *account,
			Regions:         []string{*region},
			InventoryDetail: awsscan.InventoryDetailSummary,
			CreatedAt:       time.Now().UTC(),
			RequestedBy:     "devscan",
		}
		content, err := json.MarshalIndent(req, "", "  ")
		if err != nil {
			log.Fatalf("marshal request: %v", err)
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{
				LogicalPath: paths.ScanRequest(*pusher, *scanID),
				Content:     content,
			}},
			Context: guardianapi.MutationContext{
				PrincipalID:   "devscan",
				Reason:        "create aws scan",
				CorrelationID: *scanID,
			},
		}); err != nil {
			log.Fatalf("write scan request: %v", err)
		}
		fmt.Println(*scanID)
	case "read":
		if *scanID == "" {
			log.Fatal("--scan-id is required")
		}
		raw, err := store.ReadFile(ctx, paths.ScanResult(*pusher, *scanID))
		if err != nil {
			log.Fatalf("read scan result: %v", err)
		}
		_, _ = os.Stdout.Write(raw)
	case "list":
		entries, err := store.ListDir(ctx, paths.ScanResultsDir(*pusher))
		if err != nil {
			log.Fatalf("list scan results: %v", err)
		}
		for _, entry := range entries {
			fmt.Println(entry.Name)
		}
	default:
		log.Fatalf("unknown mode %q", mode)
	}
}
