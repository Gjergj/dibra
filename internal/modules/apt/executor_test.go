package apt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestAptCacheMtimeUsesListFilesWhenDirectoryIsStale(t *testing.T) {
	root := t.TempDir()
	lists := filepath.Join(root, "lists")
	if err := os.MkdirAll(lists, 0o755); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lists, old, old); err != nil {
		t.Fatal(err)
	}

	listFile := filepath.Join(lists, "ubuntu_dists_jammy_InRelease")
	if err := os.WriteFile(listFile, []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}

	mtime, ok := newestAptCacheMtime("", "", lists)
	if !ok {
		t.Fatal("expected a cache mtime from list files")
	}
	if time.Since(mtime) > time.Minute {
		t.Fatalf("list file mtime = %v, want recent", mtime)
	}
	if !aptCacheFresh(3600, mtime, true) {
		t.Fatal("cache should be fresh when list files were just written")
	}
}

func TestNewestAptCacheMtimeIgnoresLockAndPartial(t *testing.T) {
	root := t.TempDir()
	lists := filepath.Join(root, "lists")
	partial := filepath.Join(lists, "partial")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lists, "lock"), []byte("lock"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "tmp"), []byte("tmp"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lists, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(lists, "lock"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	mtime, ok := newestAptCacheMtime("", "", lists)
	if !ok {
		t.Fatal("expected directory mtime fallback")
	}
	if time.Since(mtime) < time.Hour {
		t.Fatalf("mtime = %v, lock file should not make an empty cache look fresh", mtime)
	}
}

func TestNewestAptCacheMtimePrefersStampOverStaleListsDir(t *testing.T) {
	root := t.TempDir()
	lists := filepath.Join(root, "lists")
	stamp := filepath.Join(root, "update-success-stamp")
	if err := os.MkdirAll(lists, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stamp, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lists, old, old); err != nil {
		t.Fatal(err)
	}

	mtime, ok := newestAptCacheMtime(stamp, "", lists)
	if !ok {
		t.Fatal("expected stamp mtime")
	}
	if time.Since(mtime) > time.Minute {
		t.Fatalf("stamp mtime = %v, want recent", mtime)
	}
}

func TestTouchFileUpdatesMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "periodic", "update-success-stamp")
	if err := touchFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("stamp mtime = %v, want recent", info.ModTime())
	}
}

func aptCacheFresh(validSeconds int, mtime time.Time, ok bool) bool {
	if !ok || validSeconds <= 0 {
		return false
	}
	return time.Since(mtime) < time.Duration(validSeconds)*time.Second
}
