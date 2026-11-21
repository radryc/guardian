// Package storetest provides a behaviour conformance suite for
// guardianapi.Store implementations. Every store backend must run it so paging,
// versioning and optimistic-concurrency semantics cannot drift apart.
package storetest

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/radryc/guardian/pkg/guardianapi"
)

// Open returns a fresh, empty store for one subtest.
type Open func(t *testing.T) guardianapi.Store

// Run executes the conformance suite against the store produced by open.
func Run(t *testing.T, open Open) {
	t.Helper()

	t.Run("write and read round-trip", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		content := []byte("hello guardian")
		rev, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: "/partitions/demo/config.yaml", Content: content}},
			Context: guardianapi.MutationContext{
				PrincipalID: "tester",
				Reason:      "conformance",
			},
		})
		if err != nil {
			t.Fatalf("UpsertFiles: %v", err)
		}
		if len(rev.Files) != 1 || rev.Files[0].VersionID == "" {
			t.Fatalf("BatchRevision = %+v, want one versioned file", rev)
		}

		got, err := store.ReadFile(ctx, "/partitions/demo/config.yaml")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(got) != string(content) {
			t.Fatalf("ReadFile = %q, want %q", got, content)
		}

		versioned, version, exists, err := store.ReadFileVersioned(ctx, "/partitions/demo/config.yaml")
		if err != nil || !exists {
			t.Fatalf("ReadFileVersioned = (%q, %+v, %v, %v)", versioned, version, exists, err)
		}
		if version.VersionID != rev.Files[0].VersionID {
			t.Fatalf("version = %q, want %q", version.VersionID, rev.Files[0].VersionID)
		}

		info, err := store.Stat(ctx, "/partitions/demo/config.yaml")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.VersionID != version.VersionID || info.Size != int64(len(content)) {
			t.Fatalf("Stat = %+v, want version %q size %d", info, version.VersionID, len(content))
		}
	})

	t.Run("optimistic concurrency", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		path := "/partitions/cas/config.yaml"
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte("v1")}},
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		_, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte("stale"), ExpectedVersionID: "not-the-current-version"}},
		})
		if !errors.Is(err, guardianapi.ErrConflict) {
			t.Fatalf("stale write error = %v, want ErrConflict", err)
		}

		_, err = store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte("created"), ExpectedVersionID: "absent"}},
		})
		if !errors.Is(err, guardianapi.ErrConflict) {
			t.Fatalf("absent write over existing path error = %v, want ErrConflict", err)
		}
	})

	t.Run("delete hides path", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		path := "/partitions/gone/config.yaml"
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte("x")}},
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: []guardianapi.PathDelete{{LogicalPath: path}},
		}); err != nil {
			t.Fatalf("DeletePaths: %v", err)
		}
		if _, err := store.ReadFile(ctx, path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ReadFile after delete = %v, want os.ErrNotExist", err)
		}
		_, _, exists, err := store.ReadFileVersioned(ctx, path)
		if err != nil || exists {
			t.Fatalf("ReadFileVersioned after delete = (exists=%v, err=%v), want absent", exists, err)
		}
	})

	t.Run("paged listing matches full listing", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		dir := "/partitions/paged"
		want := []string{"a.yaml", "b.yaml", "c.yaml", "d.yaml", "e.yaml"}
		writes := make([]guardianapi.PathWrite, 0, len(want))
		for _, name := range want {
			writes = append(writes, guardianapi.PathWrite{LogicalPath: dir + "/" + name, Content: []byte(name)})
		}
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: writes}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		got := make([]string, 0, len(want))
		offset := 0
		for {
			page, err := store.ListDirPage(ctx, dir, guardianapi.DirListOptions{Offset: offset, Limit: 2})
			if err != nil {
				t.Fatalf("ListDirPage: %v", err)
			}
			for _, entry := range page.Entries {
				got = append(got, entry.Name)
			}
			if !page.HasMore {
				break
			}
			offset = page.NextOffset
		}
		sort.Strings(got)
		if len(got) != len(want) {
			t.Fatalf("paged entries = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("paged entries = %v, want %v", got, want)
			}
		}
	})

	t.Run("version history", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		path := "/partitions/history/config.yaml"
		for _, content := range []string{"one", "two"} {
			if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
				Writes: []guardianapi.PathWrite{{LogicalPath: path, Content: []byte(content)}},
			}); err != nil {
				t.Fatalf("write %q: %v", content, err)
			}
		}
		versions, err := store.ListVersions(ctx, path)
		if err != nil {
			t.Fatalf("ListVersions: %v", err)
		}
		if len(versions) < 2 {
			t.Fatalf("ListVersions returned %d entries, want >= 2", len(versions))
		}
		file, err := store.GetVersion(ctx, path, versions[1].VersionID)
		if err != nil {
			t.Fatalf("GetVersion: %v", err)
		}
		if string(file.Content) != "one" {
			t.Fatalf("GetVersion content = %q, want %q", file.Content, "one")
		}
	})
}
