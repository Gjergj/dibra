package docker_image

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

type imageClient struct {
	client.APIClient
	images        map[string]client.ImageInspectResult
	pulledImage   client.ImageInspectResult
	pullOptions   client.ImagePullOptions
	removeOptions client.ImageRemoveOptions
	removed       []string
	tagged        []client.ImageTagOptions
	closed        bool
}

func (fake *imageClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	inspect, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: not found", errdefs.ErrNotFound)
	}
	return inspect, nil
}

func (fake *imageClient) ImagePull(_ context.Context, reference string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	fake.pullOptions = options
	fake.images[reference] = fake.pulledImage
	return &pullResponse{ReadCloser: io.NopCloser(strings.NewReader(
		`{"status":"Digest: sha256:abc"}` + "\n",
	))}, nil
}

func (fake *imageClient) ImageRemove(_ context.Context, reference string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	fake.removeOptions = options
	fake.removed = append(fake.removed, reference)
	delete(fake.images, reference)
	return client.ImageRemoveResult{}, nil
}

func (fake *imageClient) ImageTag(_ context.Context, options client.ImageTagOptions) (client.ImageTagResult, error) {
	fake.tagged = append(fake.tagged, options)
	fake.images[options.Target] = fake.images[options.Source]
	return client.ImageTagResult{}, nil
}

func (fake *imageClient) Close() error {
	fake.closed = true
	return nil
}

type pullResponse struct {
	io.ReadCloser
}

func (*pullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

func (*pullResponse) Wait(context.Context) error { return nil }

func imageInspect(id string, tags ...string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: id, RepoTags: tags}}
}

func imageDependencies(fake *imageClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	}
}

func TestPullReturnsRawInspectAndHonorsPlatform(t *testing.T) {
	fake := &imageClient{
		images:      map[string]client.ImageInspectResult{},
		pulledImage: imageInspect("sha256:new", "alpine:3.19"),
	}
	response := ExecuteWithDependencies(Request{
		Name:   "alpine",
		Tag:    "3.19",
		State:  "present",
		Source: "pull",
		Pull:   &PullOptions{Platform: "linux/amd64"},
	}, imageDependencies(fake))

	if response.Failed || !response.Changed || response.Image["Id"] != "sha256:new" {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.pullOptions.Platforms) != 1 ||
		fake.pullOptions.Platforms[0].OS != "linux" ||
		fake.pullOptions.Platforms[0].Architecture != "amd64" {
		t.Fatalf("pull platforms = %#v", fake.pullOptions.Platforms)
	}
	if len(response.Actions) != 1 || response.Actions[0] != "Pulled image alpine:3.19" {
		t.Fatalf("actions = %#v", response.Actions)
	}
	if !fake.closed {
		t.Fatal("client was not closed")
	}
}

func TestForcePullComparesImageIDs(t *testing.T) {
	existing := imageInspect("sha256:same", "alpine:latest")
	fake := &imageClient{
		images:      map[string]client.ImageInspectResult{"alpine:latest": existing},
		pulledImage: existing,
	}
	response := ExecuteWithDependencies(Request{
		Name:        "alpine",
		State:       "present",
		Source:      "pull",
		ForceSource: true,
	}, imageDependencies(fake))

	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
}

func TestCheckModeRemovalDoesNotCallEngine(t *testing.T) {
	fake := &imageClient{images: map[string]client.ImageInspectResult{
		"alpine:latest": imageInspect("sha256:old", "alpine:latest"),
	}}
	response := ExecuteWithDependenciesAndState(Request{
		Name:  "alpine",
		State: "absent",
	}, imageDependencies(fake), execution.State{CheckMode: true})

	if response.Failed || !response.Changed || response.Image["state"] != "Deleted" {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("removed = %#v", fake.removed)
	}
}

func TestLocalTagIsIdempotentByTargetPresence(t *testing.T) {
	source := imageInspect("sha256:source", "source:v1")
	target := imageInspect("sha256:different", "target:v1")
	fake := &imageClient{images: map[string]client.ImageInspectResult{
		"source:v1": source,
		"target:v1": target,
	}}
	response := ExecuteWithDependencies(Request{
		Name:       "source:v1",
		State:      "present",
		Source:     "local",
		Repository: "target:v1",
	}, imageDependencies(fake))

	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.tagged) != 0 {
		t.Fatalf("tag calls = %#v", fake.tagged)
	}
}

func TestValidationRejectsMissingSourceBeforeClientCreation(t *testing.T) {
	created := false
	response := ExecuteWithDependencies(Request{Name: "alpine", State: "present"}, docker.Dependencies{
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return nil, errors.New("must not be called")
		},
	})
	if !response.Failed || !strings.Contains(response.Msg, "source is required") {
		t.Fatalf("response = %#v", response)
	}
	if created {
		t.Fatal("client was created")
	}
}

func TestCheckModeStillValidatesBuildPath(t *testing.T) {
	fake := &imageClient{images: map[string]client.ImageInspectResult{}}
	response := ExecuteWithDependenciesAndState(Request{
		Name:   "example",
		State:  "present",
		Source: "build",
		Build:  &BuildOptions{Path: filepath.Join(t.TempDir(), "missing")},
	}, imageDependencies(fake), execution.State{CheckMode: true})
	if !response.Failed || !strings.Contains(response.Msg, "could not be found") {
		t.Fatalf("response = %#v", response)
	}
}

func TestPullOptionsRetainCompatibilityPolicies(t *testing.T) {
	for input, expected := range map[string]string{
		`"always"`: "always",
		`true`:     "always",
		`false`:    "missing",
	} {
		var options PullOptions
		if err := options.UnmarshalJSON([]byte(input)); err != nil {
			t.Fatalf("UnmarshalJSON(%s): %v", input, err)
		}
		if options.Policy != expected {
			t.Fatalf("UnmarshalJSON(%s).Policy = %q, want %q", input, options.Policy, expected)
		}
	}
}

type dockerConfigFileSystem struct {
	docker.FileSystem
	config []byte
}

func (fileSystem dockerConfigFileSystem) UserHomeDir() (string, error) {
	return "/home/test", nil
}

func (fileSystem dockerConfigFileSystem) ReadFile(path string) ([]byte, error) {
	if path == "/home/test/.docker/config.json" {
		return fileSystem.config, nil
	}
	return nil, os.ErrNotExist
}

func TestDockerConfigProvidesBuildAuthAndProxyArguments(t *testing.T) {
	fileSystem := dockerConfigFileSystem{config: []byte(`{
		"auths":{"registry.test":{"auth":"dXNlcjpwYXNz"}},
		"proxies":{"default":{"httpProxy":"http://proxy.test","noProxy":"localhost"}}
	}`)}
	dependencies := docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		FileSystem:  fileSystem,
	}
	auth, err := dockerConfigAuthConfigs(Request{}, "registry.test/team/image:latest", dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if auth["registry.test"].Username != "user" || auth["registry.test"].Password != "pass" {
		t.Fatalf("auth = %#v", auth)
	}
	proxy := dockerConfigProxyArgs(Request{}, dependencies)
	if proxy["HTTP_PROXY"] != "http://proxy.test" || proxy["http_proxy"] != "http://proxy.test" ||
		proxy["NO_PROXY"] != "localhost" || proxy["no_proxy"] != "localhost" {
		t.Fatalf("proxy = %#v", proxy)
	}
}

func TestBuildContextArchiveHonorsDockerIgnore(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"Dockerfile":    "FROM scratch\n",
		".dockerignore": "ignored.txt\n",
		"included.txt":  "included",
		"ignored.txt":   "ignored",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := buildContextArchive(root, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	names := map[string]bool{}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[header.Name] = true
	}
	if !names["Dockerfile"] || !names[".dockerignore"] || !names["included.txt"] || names["ignored.txt"] {
		t.Fatalf("archive names = %#v", names)
	}
}
