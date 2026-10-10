package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
	return err == nil
}

func TestSyncTreeRemovesFilesDeletedFromSource(t *testing.T) {
	dst := t.TempDir()

	v1 := t.TempDir()
	writeFiles(t, v1, map[string]string{"main.go": "v1", "internal/drift/drift.go": "x", "internal/keep.go": "k"})
	if err := syncTree(v1, dst); err != nil {
		t.Fatal(err)
	}
	// A container writes data into the project folder.
	writeFiles(t, dst, map[string]string{"data/db.sqlite": "rows", "internal/drift/cache.bin": "c"})

	v2 := t.TempDir()
	writeFiles(t, v2, map[string]string{"main.go": "v2", "internal/keep.go": "k"})
	if err := syncTree(v2, dst); err != nil {
		t.Fatal(err)
	}
	if exists(dst, "internal/drift/drift.go") {
		t.Fatal("a file deleted from the source survived the redeploy")
	}
	if !exists(dst, "data/db.sqlite") || !exists(dst, "internal/drift/cache.bin") {
		t.Fatal("container data was deleted")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "main.go")); string(b) != "v2" {
		t.Fatalf("main.go not updated: %q", b)
	}

	// An emptied directory goes away.
	v3 := t.TempDir()
	writeFiles(t, v3, map[string]string{"main.go": "v3"})
	if err := syncTree(v3, dst); err != nil {
		t.Fatal(err)
	}
	if exists(dst, "internal/keep.go") || !exists(dst, "internal/drift/cache.bin") {
		t.Fatal("internal/keep.go should be gone, container data kept")
	}
}

func TestSyncTreeFirstRunDeletesNothing(t *testing.T) {
	dst := t.TempDir()
	writeFiles(t, dst, map[string]string{"old.go": "from an earlier agent"})
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"main.go": "v1"})
	if err := syncTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if !exists(dst, "old.go") {
		t.Fatal("without a manifest nothing may be deleted")
	}
}

func TestSyncTreeIgnoresManifestEscapes(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"secret": "s"})
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, manifestName), []byte("../"+filepath.Base(outside)+"/secret\n/etc/passwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"main.go": "v1"})
	if err := syncTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if !exists(outside, "secret") {
		t.Fatal("manifest entry outside the project deleted a file")
	}
}
