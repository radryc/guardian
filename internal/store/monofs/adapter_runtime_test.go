package monofs

import (
	"context"
	"testing"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func TestAdapterReadOperationsUsePhysicalPaths(t *testing.T) {
	t.Parallel()

	client := &fakeMonoFSClient{
		readContent: []byte("ok"),
		listEntries: []guardianapi.DirEntry{{Name: "shared", IsDir: true}},
	}
	store := New(client, "token")

	if _, err := store.ReadFile(context.Background(), "/partitions/shared/config.yaml"); err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if client.readPath != "guardian/shared/config.yaml" {
		t.Fatalf("ReadFile path = %q, want %q", client.readPath, "guardian/shared/config.yaml")
	}

	if _, err := store.ListDir(context.Background(), "/partitions"); err != nil {
		t.Fatalf("ListDir() error = %v", err)
	}
	if client.listPath != "guardian" {
		t.Fatalf("ListDir path = %q, want %q", client.listPath, "guardian")
	}
}

func TestAdapterListDirPageUsesPhysicalPaths(t *testing.T) {
	t.Parallel()

	client := &fakeMonoFSClient{
		listEntries: []guardianapi.DirEntry{
			{Name: "a", IsDir: true},
			{Name: "b", IsDir: true},
		},
	}
	store := New(client, "token")

	page, err := store.ListDirPage(context.Background(), "/.archive/demo/api", guardianapi.DirListOptions{
		Offset: 1,
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("ListDirPage() error = %v", err)
	}
	if client.listPath != "guardian-system/.archive/demo/api" {
		t.Fatalf("ListDirPage path = %q, want %q", client.listPath, "guardian-system/.archive/demo/api")
	}
	if got, want := len(page.Entries), 1; got != want {
		t.Fatalf("page size = %d, want %d", got, want)
	}
	if got, want := page.Entries[0].Name, "b"; got != want {
		t.Fatalf("entry name = %q, want %q", got, want)
	}
}

func TestAdapterWriteOperationsKeepLogicalPaths(t *testing.T) {
	t.Parallel()

	client := &fakeMonoFSClient{
		upsertResp: guardianapi.BatchRevision{
			Files: []guardianapi.FileVersion{{LogicalPath: "/partitions/shared/config.yaml", VersionID: "v1"}},
		},
		deleteResp: guardianapi.BatchRevision{
			Files: []guardianapi.FileVersion{{LogicalPath: "/partitions/shared/config.yaml", VersionID: "v2"}},
		},
	}
	store := New(client, "token")

	_, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: "/partitions/shared/config.yaml",
			Content:     []byte("config"),
		}},
	})
	if err != nil {
		t.Fatalf("UpsertFiles() error = %v", err)
	}
	if got := client.upsertWrites[0].LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("UpsertFiles path = %q, want logical path", got)
	}

	_, err = store.DeletePaths(context.Background(), guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{
			LogicalPath: "/partitions/shared/config.yaml",
		}},
	})
	if err != nil {
		t.Fatalf("DeletePaths() error = %v", err)
	}
	if got := client.deletePaths[0].LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("DeletePaths path = %q, want logical path", got)
	}
}

func TestAdapterVersionOperationsPreserveLogicalPaths(t *testing.T) {
	t.Parallel()

	client := &fakeMonoFSClient{
		versionsResp: []guardianapi.FileVersion{{
			LogicalPath: "/partitions/shared/config.yaml",
			VersionID:   "v1",
		}},
		getVersionResp: guardianapi.VersionedFile{
			Version: guardianapi.FileVersion{
				LogicalPath: "/partitions/shared/config.yaml",
				VersionID:   "v1",
			},
			Content: []byte("config"),
		},
	}
	store := New(client, "token")

	versions, err := store.ListVersions(context.Background(), "/partitions/shared/config.yaml")
	if err != nil {
		t.Fatalf("ListVersions() error = %v", err)
	}
	if got := client.versionsPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("ListVersions path = %q, want logical path", got)
	}
	if got := versions[0].LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("ListVersions returned path = %q, want logical path", got)
	}

	versioned, err := store.GetVersion(context.Background(), "/partitions/shared/config.yaml", "v1")
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}
	if got := client.getVersionPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("GetVersion path = %q, want logical path", got)
	}
	if got := versioned.Version.LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("GetVersion returned path = %q, want logical path", got)
	}
}

func TestAdapterWatchKeepsLogicalPrefixes(t *testing.T) {
	t.Parallel()

	client := &fakeMonoFSClient{
		watchCh: make(chan guardianapi.ChangeEvent, 1),
	}
	store := New(client, "token")

	ch, err := store.Watch(context.Background(), []string{"/partitions/shared"})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if len(client.watchPrefixes) != 1 || client.watchPrefixes[0] != "/partitions/shared" {
		t.Fatalf("Watch prefixes = %v, want logical prefixes", client.watchPrefixes)
	}

	client.watchCh <- guardianapi.ChangeEvent{LogicalPath: "/partitions/shared/config.yaml"}
	event := <-ch
	if got := event.LogicalPath; got != "/partitions/shared/config.yaml" {
		t.Fatalf("Watch event path = %q, want logical path", got)
	}
}

type fakeMonoFSClient struct {
	readContent    []byte
	listEntries    []guardianapi.DirEntry
	upsertResp     guardianapi.BatchRevision
	deleteResp     guardianapi.BatchRevision
	versionsResp   []guardianapi.FileVersion
	getVersionResp guardianapi.VersionedFile
	watchCh        chan guardianapi.ChangeEvent

	readPath       string
	listPath       string
	upsertWrites   []guardianapi.PathWrite
	deletePaths    []guardianapi.PathDelete
	versionsPath   string
	getVersionPath string
	watchPrefixes  []string
}

func (f *fakeMonoFSClient) UpsertPaths(_ context.Context, _ string, writes []guardianapi.PathWrite, _ guardianapi.MutationContext) (guardianapi.BatchRevision, error) {
	f.upsertWrites = append([]guardianapi.PathWrite(nil), writes...)
	return f.upsertResp, nil
}

func (f *fakeMonoFSClient) DeletePaths(_ context.Context, _ string, deletes []guardianapi.PathDelete, _ guardianapi.MutationContext) (guardianapi.BatchRevision, error) {
	f.deletePaths = append([]guardianapi.PathDelete(nil), deletes...)
	return f.deleteResp, nil
}

func (f *fakeMonoFSClient) ListVersions(_ context.Context, _ string, logicalPath string) ([]guardianapi.FileVersion, error) {
	f.versionsPath = logicalPath
	return append([]guardianapi.FileVersion(nil), f.versionsResp...), nil
}

func (f *fakeMonoFSClient) GetVersion(_ context.Context, _ string, logicalPath, _ string) (guardianapi.VersionedFile, error) {
	f.getVersionPath = logicalPath
	return f.getVersionResp, nil
}

func (f *fakeMonoFSClient) ReadFile(_ context.Context, mountPath string) ([]byte, error) {
	f.readPath = mountPath
	return append([]byte(nil), f.readContent...), nil
}

func (f *fakeMonoFSClient) ListDir(_ context.Context, mountPath string) ([]guardianapi.DirEntry, error) {
	f.listPath = mountPath
	return append([]guardianapi.DirEntry(nil), f.listEntries...), nil
}

func (f *fakeMonoFSClient) ListDirPage(_ context.Context, mountPath string, opts guardianapi.DirListOptions) (guardianapi.DirListPage, error) {
	f.listPath = mountPath
	if opts.Offset >= len(f.listEntries) {
		return guardianapi.DirListPage{Entries: nil, NextOffset: opts.Offset, HasMore: false}, nil
	}
	end := len(f.listEntries)
	if opts.Limit > 0 && opts.Offset+opts.Limit < end {
		end = opts.Offset + opts.Limit
	}
	page := guardianapi.DirListPage{
		Entries: append([]guardianapi.DirEntry(nil), f.listEntries[opts.Offset:end]...),
	}
	if end < len(f.listEntries) {
		page.HasMore = true
		page.NextOffset = end
	} else {
		page.NextOffset = end
	}
	return page, nil
}

func (f *fakeMonoFSClient) Watch(_ context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	f.watchPrefixes = append([]string(nil), prefixes...)
	return f.watchCh, nil
}

var _ MonoFSClient = (*fakeMonoFSClient)(nil)
var _ watcherClient = (*fakeMonoFSClient)(nil)
var _ pagedMonoFSClient = (*fakeMonoFSClient)(nil)
