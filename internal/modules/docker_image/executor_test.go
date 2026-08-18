package docker_image

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
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
	archive       []byte
	buildOptions  client.ImageBuildOptions
	buildContext  io.Reader
	buildStream   string
	builtImage    client.ImageInspectResult
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

func (fake *imageClient) ImageSave(_ context.Context, _ []string, _ ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	return io.NopCloser(bytes.NewReader(fake.archive)), nil
}

func (fake *imageClient) ImageBuild(_ context.Context, context io.Reader, options client.ImageBuildOptions) (client.ImageBuildResult, error) {
	fake.buildContext = context
	fake.buildOptions = options
	if fake.builtImage.ID != "" && len(options.Tags) > 0 {
		fake.images[options.Tags[0]] = fake.builtImage
	}
	return client.ImageBuildResult{Body: io.NopCloser(strings.NewReader(fake.buildStream))}, nil
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
		Environment: docker.StaticEnvironment{"DOCKER_CONFIG": "/nonexistent/dibra-image-unit-test"},
		FileSystem:  docker.OSFileSystem{},
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

	if response.Failed || !response.Changed || response.Image["state"] != "Deleted" || len(response.Image) != 1 {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("removed = %#v", fake.removed)
	}
}

func TestFailedResponseOmitsSuccessFieldsButPreservesLoadStdout(t *testing.T) {
	response := failedResponse("boom")
	response.Stdout = "load output"
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "load output" {
		t.Fatalf("stdout = %#v", result["stdout"])
	}
	for _, field := range []string{"actions", "image"} {
		if _, found := result[field]; found {
			t.Fatalf("failed response contains %q: %s", field, data)
		}
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

func TestCheckModeMissingSourceWithArchivePreservesPredictedChange(t *testing.T) {
	fake := &imageClient{images: map[string]client.ImageInspectResult{}}
	archivePath := filepath.Join(t.TempDir(), "image.tar")
	response := ExecuteWithDependenciesAndState(Request{
		Name: "example", State: "present", Source: "pull", ArchivePath: archivePath,
	}, imageDependencies(fake), execution.State{CheckMode: true})
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("check mode created archive: %v", err)
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

func TestBuildValueConversionMatchesPinnedUpstream(t *testing.T) {
	arguments := stringifyPointerMap(map[string]any{
		"boolean": true,
		"null":    nil,
		"list":    []any{"one", false},
	})
	if *arguments["boolean"] != "True" || *arguments["null"] != "None" ||
		*arguments["list"] != "['one', False]" {
		t.Fatalf("build arguments = %#v", arguments)
	}
	labels := stringifyMap(map[string]any{"boolean": true, "null": nil})
	if labels["boolean"] != "true" || labels["null"] != "None" {
		t.Fatalf("build labels = %#v", labels)
	}
	hosts := extraHosts(map[string]any{"enabled": false, "null": nil})
	if strings.Join(hosts, ",") != "enabled:false,null:None" {
		t.Fatalf("extra hosts = %#v", hosts)
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
	auth, err := dockerConfigAuthConfigs(context.Background(), Request{}, "registry.test/team/image:latest", dependencies)
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
	archive, _, err := buildContextArchive(root, "", docker.OSFileSystem{})
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

func TestBuildContextArchiveSupportsNegationsAndSelectedDockerfile(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"ignored", "custom", "other"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		".dockerignore":       "ignored/\n!ignored/kept.txt\ncustom/*\n**/Dockerfile\n",
		"Dockerfile":          "FROM scratch\n",
		"ignored/kept.txt":    "keep",
		"ignored/dropped.txt": "drop",
		"custom/Buildfile":    "FROM busybox\n",
		"other/Dockerfile":    "FROM alpine\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive, effective, err := buildContextArchive(root, "custom/Buildfile", docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	names := archiveEntries(t, archive)
	if effective != "custom/Buildfile" || !names["custom/Buildfile"] || !names["ignored/kept.txt"] ||
		names["ignored/dropped.txt"] || names["other/Dockerfile"] {
		t.Fatalf("effective Dockerfile = %q; archive names = %#v", effective, names)
	}
}

func TestBuildContextArchiveInjectsDockerfileOutsideContext(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "OutsideDockerfile")
	if err := os.WriteFile(outside, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, effective, err := buildContextArchive(root, outside, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := archiveEntries(t, archive)
	if !strings.HasPrefix(effective, ".dockerfile.") || !entries[effective] || !entries[".dockerignore"] {
		t.Fatalf("effective Dockerfile = %q; archive names = %#v", effective, entries)
	}
}

func TestRemoteBuildContextIsForwardedWithoutFilesystemValidation(t *testing.T) {
	reference := "example:latest"
	fake := &imageClient{
		images:      map[string]client.ImageInspectResult{},
		buildStream: `{"stream":"Successfully built new\n"}`,
		builtImage:  imageInspect("sha256:new", reference),
	}
	response := ExecuteWithDependencies(Request{
		Name: "example", State: "present", Source: "build",
		Build: &BuildOptions{Path: "https://example.test/context.git", Dockerfile: "Dockerfile.custom"},
	}, imageDependencies(fake))
	if response.Failed || !response.Changed || fake.buildContext != nil ||
		fake.buildOptions.RemoteContext != "https://example.test/context.git" ||
		fake.buildOptions.Dockerfile != "Dockerfile.custom" {
		t.Fatalf("response = %#v; build options = %#v; context = %#v",
			response, fake.buildOptions, fake.buildContext)
	}
}

func archiveEntries(t *testing.T, archive io.Reader) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[header.Name] = true
	}
}

func TestValidationRejectsImageIDsForPullBuildRepositoryAndPush(t *testing.T) {
	id := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "pull", req: Request{Name: id, Source: "pull", ForceSource: true}, want: "Image name must not be an image ID for source=pull"},
		{name: "build", req: Request{Name: id, Source: "build", ForceSource: true, Build: &BuildOptions{Path: t.TempDir()}}, want: "Image name must not be an image ID for source=build"},
		{name: "repository", req: Request{Name: "alpine:latest", Source: "local", Repository: id}, want: "`repository` must not be an image ID"},
		{name: "push", req: Request{Name: id, Source: "local", Push: true}, want: "Cannot push an image"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &imageClient{images: map[string]client.ImageInspectResult{
				id:              imageInspect(id),
				"alpine:latest": imageInspect("sha256:abc", "alpine:latest"),
			}}
			response := ExecuteWithDependencies(test.req, docker.Dependencies{
				Environment: docker.StaticEnvironment{},
				NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
			})
			if !response.Failed || !strings.Contains(response.Msg, test.want) {
				t.Fatalf("ExecuteWithDependencies() = %#v, want %q", response, test.want)
			}
		})
	}
}

func TestArchivedImageActionMatchesPinnedUpstreamCases(t *testing.T) {
	root := t.TempDir()
	fake := &imageClient{
		images: map[string]client.ImageInspectResult{
			"a:latest": imageInspect("sha256:a1", "a:latest"),
			"b:latest": imageInspect("sha256:b2", "b:latest"),
			"c:1.2.3":  imageInspect("sha256:c3", "c:1.2.3"),
			"d:0.0.1":  imageInspect("sha256:d4", "d:0.0.1"),
		},
	}
	dependencies := docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	}

	missingPath := filepath.Join(root, "missing.tar")
	missing := ExecuteWithDependencies(Request{Name: "a:latest", Source: "local", ArchivePath: missingPath}, dependencies)
	if missing.Failed || !missing.Changed || !strings.Contains(strings.Join(missing.Actions, "\n"), "since none present") {
		t.Fatalf("missing archive = %#v", missing)
	}

	currentPath := filepath.Join(root, "current.tar")
	if err := os.WriteFile(currentPath, imageArchiveManifest(t, "b2", []string{"b:latest"}), 0o600); err != nil {
		t.Fatal(err)
	}
	current := ExecuteWithDependencies(Request{Name: "b:latest", Source: "local", ArchivePath: currentPath}, dependencies)
	if current.Failed || current.Changed {
		t.Fatalf("current archive = %#v", current)
	}

	invalidPath := filepath.Join(root, "invalid.tar")
	if err := os.WriteFile(invalidPath, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := ExecuteWithDependencies(Request{Name: "c:1.2.3", Source: "local", ArchivePath: invalidPath}, dependencies)
	if invalid.Failed || !invalid.Changed || !strings.Contains(strings.Join(invalid.Actions, "\n"), "overwriting an unreadable archive file") {
		t.Fatalf("invalid archive = %#v", invalid)
	}

	obsoleteIDPath := filepath.Join(root, "obsolete-id.tar")
	if err := os.WriteFile(obsoleteIDPath, imageArchiveManifest(t, "e5", []string{"d:0.0.1"}), 0o600); err != nil {
		t.Fatal(err)
	}
	obsoleteID := ExecuteWithDependencies(Request{Name: "d:0.0.1", Source: "local", ArchivePath: obsoleteIDPath}, dependencies)
	if obsoleteID.Failed || !obsoleteID.Changed || !strings.Contains(strings.Join(obsoleteID.Actions, "\n"), "overwriting archive with image e5 named d:0.0.1") {
		t.Fatalf("obsolete id archive = %#v", obsoleteID)
	}

	obsoleteNamePath := filepath.Join(root, "obsolete-name.tar")
	if err := os.WriteFile(obsoleteNamePath, imageArchiveManifest(t, "d4", []string{"hi"}), 0o600); err != nil {
		t.Fatal(err)
	}
	obsoleteName := ExecuteWithDependencies(Request{Name: "d:0.0.1", Source: "local", ArchivePath: obsoleteNamePath}, dependencies)
	if obsoleteName.Failed || !obsoleteName.Changed || !strings.Contains(strings.Join(obsoleteName.Actions, "\n"), "overwriting archive with image d4 named hi") {
		t.Fatalf("obsolete name archive = %#v", obsoleteName)
	}
}

func imageArchiveManifest(t *testing.T, imageID string, tags []string) []byte {
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
