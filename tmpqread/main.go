package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	monofsstore "github.com/radryc/guardian/internal/store/monofs"
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
	dir := "/.queues/" + os.Args[3] + "/.results"
	entries, err := store.ListDir(ctx, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}
	fmt.Printf("results dir: %d entries\n", len(entries))
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name > entries[j].Name })
	for i, e := range entries {
		if i >= 2 {
			break
		}
		data, err := store.ReadFile(ctx, dir+"/"+e.Name)
		if err != nil {
			continue
		}
		var r struct{ TaskID, Op, Status, Error string }
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		if r.Op != "APPLY" {
			continue
		}
		fmt.Printf("task=%s status=%s\n", r.TaskID, r.Status)
		if len(r.Error) > 1800 {
			fmt.Println(r.Error[len(r.Error)-1800:])
		} else {
			fmt.Println(r.Error)
		}
	}
}
