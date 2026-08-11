package docker

import (
	"archive/tar"
	"bytes"
	"testing"
)

func imageArchiveFixture(t *testing.T, manifest string) *bytes.Reader {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(archive.Bytes())
}

func TestReadImageArchiveManifest(t *testing.T) {
	manifest, err := ReadImageArchiveManifest(imageArchiveFixture(t, `[
		{"Config":"abcde12345.json","RepoTags":["foo:latest"]},
		{"Config":"blobs/sha256/67890","RepoTags":["bar:v1"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 2 || manifest[0].ImageID != "abcde12345" || manifest[1].ImageID != "67890" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !ImageArchiveMatches(manifest, map[string]string{
		"foo:latest": "sha256:abcde12345",
		"bar:v1":     "sha256:67890",
	}) {
		t.Fatal("ImageArchiveMatches() = false")
	}
}

func TestReadImageArchiveManifestRejectsInvalidArchives(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{name: "empty manifest", manifest: `[]`},
		{name: "missing config", manifest: `[{"RepoTags":["foo:latest"]}]`},
		{name: "missing repo tags", manifest: `[{"Config":"abc.json"}]`},
		{name: "invalid repo tags", manifest: `[{"Config":"abc.json","RepoTags":false}]`},
		{name: "invalid json", manifest: `[`},
		{name: "trailing json", manifest: `[] {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ReadImageArchiveManifest(imageArchiveFixture(t, test.manifest)); err == nil {
				t.Fatal("ReadImageArchiveManifest() unexpectedly succeeded")
			}
		})
	}
}
