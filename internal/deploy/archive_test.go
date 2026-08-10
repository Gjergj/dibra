package deploy

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractProjectRootAndWrappedLayouts(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		prefix string
	}{
		{name: "root"},
		{name: "wrapped", prefix: "project/"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			archivePath := filepath.Join(t.TempDir(), "project.zip")
			writeTestZIP(t, archivePath, []zipEntry{
				{name: testCase.prefix + manifestName, body: "version: 1\nplaybooks:\n  - playbook.yaml\n"},
				{name: testCase.prefix + "playbook.yaml", body: "tasks:\n  - name: ping\n    ping:\n"},
			})
			project, err := ExtractProject(archivePath, filepath.Join(t.TempDir(), "out"))
			if err != nil {
				t.Fatalf("ExtractProject() error = %v", err)
			}
			if got := project.Manifest.Playbooks; len(got) != 1 || got[0] != "playbook.yaml" {
				t.Fatalf("unexpected playbooks: %#v", got)
			}
			if _, err := os.Stat(filepath.Join(project.Root, "playbook.yaml")); err != nil {
				t.Fatalf("playbook not extracted under project root: %v", err)
			}
		})
	}
}

func TestExtractProjectRejectsUnsafeEntries(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		entries   []zipEntry
		wantError string
	}{
		{
			name:      "traversal",
			entries:   []zipEntry{{name: "../escape", body: "bad"}},
			wantError: "traversal",
		},
		{
			name:      "normalized traversal",
			entries:   []zipEntry{{name: "directory/../escape", body: "bad"}},
			wantError: "traversal",
		},
		{
			name:      "Windows absolute path",
			entries:   []zipEntry{{name: "C:/escape", body: "bad"}},
			wantError: "escapes the project root",
		},
		{
			name:      "symlink",
			entries:   []zipEntry{{name: "link", body: "target", mode: os.ModeSymlink | 0o777}},
			wantError: "unsupported file type",
		},
		{
			name: "duplicate manifests",
			entries: []zipEntry{
				{name: manifestName, body: "version: 1\nplaybooks: [playbook.yaml]\n"},
				{name: "nested/" + manifestName, body: "version: 1\nplaybooks: [playbook.yaml]\n"},
			},
			wantError: "exactly one",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			archivePath := filepath.Join(t.TempDir(), "project.zip")
			writeTestZIP(t, archivePath, testCase.entries)
			_, err := ExtractProject(archivePath, filepath.Join(t.TempDir(), "out"))
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("ExtractProject() error = %v, want substring %q", err, testCase.wantError)
			}
		})
	}
}

func TestExtractProjectValidatesManifestStrictly(t *testing.T) {
	t.Parallel()
	archivePath := filepath.Join(t.TempDir(), "project.zip")
	writeTestZIP(t, archivePath, []zipEntry{
		{name: manifestName, body: "version: 1\nplaybooks: [../outside.yaml]\nunknown: true\n"},
	})
	_, err := ExtractProject(archivePath, filepath.Join(t.TempDir(), "out"))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("ExtractProject() error = %v, want strict unknown-field error", err)
	}
}

func TestExtractProjectRejectsMultipleManifestDocuments(t *testing.T) {
	t.Parallel()
	archivePath := filepath.Join(t.TempDir(), "project.zip")
	writeTestZIP(t, archivePath, []zipEntry{
		{name: manifestName, body: "version: 1\nplaybooks: [playbook.yaml]\n---\nversion: 1\nplaybooks: [other.yaml]\n"},
		{name: "playbook.yaml", body: "tasks: []\n"},
	})
	_, err := ExtractProject(archivePath, filepath.Join(t.TempDir(), "out"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("ExtractProject() error = %v, want multiple-document error", err)
	}
}

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

func writeTestZIP(t *testing.T, destination string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entryWriter.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
