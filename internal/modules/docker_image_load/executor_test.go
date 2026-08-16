package docker_image_load

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type loadClient struct {
	client.APIClient
	stream      string
	loadErr     error
	loadOptions int
	input       []byte
	images      map[string]client.ImageInspectResult
	inspected   []string
}

func (fake *loadClient) ImageLoad(_ context.Context, input io.Reader, options ...client.ImageLoadOption) (client.ImageLoadResult, error) {
	fake.loadOptions = len(options)
	fake.input, _ = io.ReadAll(input)
	if fake.loadErr != nil {
		return nil, fake.loadErr
	}
	return io.NopCloser(strings.NewReader(fake.stream)), nil
}

func (fake *loadClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	fake.inspected = append(fake.inspected, reference)
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("not found")
	}
	return image, nil
}

func (*loadClient) Close() error { return nil }

func loadInspect(id string, tags ...string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID:           id,
		RepoTags:     tags,
		Architecture: "amd64",
	}}
}

func loadDependencies(fake *loadClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func archivePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "images.tar")
	if err := os.WriteFile(path, []byte("archive bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReturnsNamesRawInspectionsAndAlwaysChanges(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	fake := &loadClient{
		stream: `{"stream":"Loaded image: alpine:latest\n"}` + "\n" +
			`{"status":"Loaded image ID: ` + id + `\n"}`,
		images: map[string]client.ImageInspectResult{
			"alpine:latest": loadInspect(id, "alpine:latest"),
			id:              loadInspect(id),
		},
	}
	path := archivePath(t)

	for iteration := 0; iteration < 2; iteration++ {
		response := ExecuteWithDependencies(Request{Path: path}, loadDependencies(fake))
		if response.Failed || !response.Changed || len(response.ImageNames) != 2 || len(response.Images) != 2 {
			t.Fatalf("iteration %d response = %#v", iteration+1, response)
		}
		if response.ImageNames[0] != "alpine:latest" || response.ImageNames[1] != id ||
			response.Images[0]["Id"] != id || response.Images[0]["Architecture"] != "amd64" {
			t.Fatalf("iteration %d result = %#v", iteration+1, response)
		}
		if response.Stdout != "Loaded image: alpine:latest\nLoaded image ID: "+id {
			t.Fatalf("stdout = %q", response.Stdout)
		}
	}
	if fake.loadOptions != 0 {
		t.Fatalf("ImageLoad received %d options; upstream uses quiet=false", fake.loadOptions)
	}
	if string(fake.input) != "archive bytes" {
		t.Fatalf("archive input = %q", fake.input)
	}
}

func TestLoadSurfacesEmbeddedStreamErrorWithPriorOutput(t *testing.T) {
	fake := &loadClient{
		stream: `{"stream":"Preparing archive\n"}` + "\n" +
			`{"errorDetail":{"code":500,"message":"corrupt layer"}}`,
	}
	response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "corrupt layer") || response.Stdout != "Preparing archive" {
		t.Fatalf("response = %#v", response)
	}
}

func TestLoadRejectsMissingCorruptAndInspectionFailures(t *testing.T) {
	t.Run("missing archive", func(t *testing.T) {
		fake := &loadClient{}
		response := ExecuteWithDependencies(Request{Path: filepath.Join(t.TempDir(), "missing.tar")}, loadDependencies(fake))
		if !response.Failed || !strings.Contains(response.Msg, "Error opening archive") {
			t.Fatalf("response = %#v", response)
		}
	})

	t.Run("no loaded images", func(t *testing.T) {
		fake := &loadClient{stream: `{"status":"Import complete"}`}
		response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
		if !response.Failed || response.Msg != "Detected no loaded images. Archive potentially corrupt?" ||
			response.Stdout != "Import complete" {
			t.Fatalf("response = %#v", response)
		}
	})

	t.Run("engine failure", func(t *testing.T) {
		fake := &loadClient{loadErr: errors.New("invalid tar header")}
		response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
		if !response.Failed || !strings.Contains(response.Msg, "invalid tar header") {
			t.Fatalf("response = %#v", response)
		}
	})

	t.Run("inspection failure", func(t *testing.T) {
		fake := &loadClient{stream: `{"stream":"Loaded image: missing:latest\n"}`}
		response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
		if !response.Failed || !strings.Contains(response.Msg, "Error inspecting loaded image") {
			t.Fatalf("response = %#v", response)
		}
	})
}

func TestUntaggedLoadedNameWarnsAndRemainsInImageNames(t *testing.T) {
	fake := &loadClient{stream: `{"stream":"Loaded image: untagged\n"}`}
	response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
	if response.Failed || !response.Changed || len(response.ImageNames) != 1 || len(response.Images) != 0 ||
		len(response.Warnings) != 1 || !strings.Contains(response.Warnings[0], "neither ID nor has a tag") {
		t.Fatalf("response = %#v", response)
	}
}

func TestLoadedImageIDRecognitionMatchesPinnedUpstream(t *testing.T) {
	valid := "sha256:" + strings.Repeat("aB", 32)
	if !isImageID(valid) {
		t.Fatalf("full canonical ID was not recognized: %s", valid)
	}
	for _, invalid := range []string{
		strings.Repeat("a", 64),
		"sha256:" + strings.Repeat("a", 12),
		"SHA256:" + strings.Repeat("a", 64),
		"sha256:" + strings.Repeat("g", 64),
	} {
		if isImageID(invalid) {
			t.Errorf("isImageID(%q) = true", invalid)
		}
	}

	bare := strings.Repeat("a", 64)
	fake := &loadClient{stream: `{"stream":"Loaded image ID: ` + bare + `\n"}`}
	response := ExecuteWithDependencies(Request{Path: archivePath(t)}, loadDependencies(fake))
	if response.Failed || !response.Changed || len(response.ImageNames) != 1 ||
		len(response.Images) != 0 || len(response.Warnings) != 1 || len(fake.inspected) != 0 {
		t.Errorf("bare ID response = %#v inspected = %#v", response, fake.inspected)
	}
}
