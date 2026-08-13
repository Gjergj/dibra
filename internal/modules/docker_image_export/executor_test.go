package docker_image_export

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type exportClient struct {
	client.APIClient
	images      map[string]client.ImageInspectResult
	saveNames   []string
	saveOptions int
	archive     []byte
}

func (fake *exportClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("not found")
	}
	return image, nil
}

func (fake *exportClient) ImageSave(_ context.Context, names []string, options ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	fake.saveNames = append([]string(nil), names...)
	fake.saveOptions = len(options)
	return io.NopCloser(bytes.NewReader(fake.archive)), nil
}

func (*exportClient) Close() error { return nil }

func exportInspect(id string, tags ...string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID:       id,
		RepoTags: tags,
	}}
}

func exportDependencies(fake *exportClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestCheckModeReturnsRawImagesWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.tar")
	fake := &exportClient{images: map[string]client.ImageInspectResult{
		"alpine:latest": exportInspect("sha256:abc", "alpine:latest"),
	}}
	response := ExecuteWithDependenciesAndState(Request{Names: []string{"alpine"}, Path: path},
		exportDependencies(fake), execution.State{CheckMode: true})

	if response.Failed || !response.Changed || response.Images[0]["Id"] != "sha256:abc" {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.saveNames) != 0 {
		t.Fatalf("ImageSave called with %#v", fake.saveNames)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("archive was created in check mode: %v", err)
	}
}

func TestMatchingArchiveIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(path, archiveManifest(t, "abc", []string{"alpine:latest"}), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &exportClient{images: map[string]client.ImageInspectResult{
		"alpine:latest": exportInspect("sha256:abc", "alpine:latest"),
	}}
	response := ExecuteWithDependencies(Request{Names: []string{"alpine"}, Path: path}, exportDependencies(fake))

	if response.Failed || response.Changed || len(fake.saveNames) != 0 {
		t.Fatalf("response = %#v; saves = %#v", response, fake.saveNames)
	}
}

func TestExportsNamesIDsAndPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "images.tar")
	archive := archiveManifest(t, "abc", []string{"alpine:latest"})
	id := "123456789abc"
	fake := &exportClient{
		images: map[string]client.ImageInspectResult{
			"alpine:latest": exportInspect("sha256:abc", "alpine:latest"),
			id:              exportInspect("sha256:def", "example:latest"),
		},
		archive: archive,
	}
	response := ExecuteWithDependencies(Request{
		Names:    []string{"alpine", id},
		Path:     path,
		Platform: "linux/amd64",
	}, exportDependencies(fake))

	if response.Failed || !response.Changed || len(response.Images) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if strings.Join(fake.saveNames, ",") != "alpine:latest,"+id || fake.saveOptions != 1 {
		t.Fatalf("save names/options = %#v/%d", fake.saveNames, fake.saveOptions)
	}
	if contents, err := os.ReadFile(path); err != nil || !bytes.Equal(contents, archive) {
		t.Fatalf("archive = %d bytes, %v", len(contents), err)
	}
}

func TestPlatformRequiresAPI148(t *testing.T) {
	version := "1.47"
	created := false
	response := ExecuteWithDependencies(Request{
		CommonArgs: docker.CommonArgs{APIVersion: &version},
		Names:      []string{"alpine"},
		Path:       "/tmp/image.tar",
		Platform:   "linux/amd64",
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return nil, nil
		},
	})
	if !response.Failed || !strings.Contains(response.Msg, "1.48") || created {
		t.Fatalf("response = %#v; created = %v", response, created)
	}
}

func TestForceAlwaysPredictsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := os.WriteFile(path, archiveManifest(t, "abc", []string{"alpine:latest"}), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &exportClient{images: map[string]client.ImageInspectResult{
		"alpine:latest": exportInspect("sha256:abc", "alpine:latest"),
	}}
	response := ExecuteWithDependenciesAndState(Request{
		Names: []string{"alpine"}, Path: path, Force: true,
	}, exportDependencies(fake), execution.State{CheckMode: true})
	if response.Failed || !response.Changed || response.Msg != "Exporting since force=true" {
		t.Fatalf("response = %#v", response)
	}
}

func archiveManifest(t *testing.T, imageID string, tags []string) []byte {
	t.Helper()
	manifest := fmt.Sprintf(`[{"Config":"%s.json","RepoTags":["%s"],"Layers":[]}]`, imageID, strings.Join(tags, `","`))
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
