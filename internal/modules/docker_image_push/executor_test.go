package docker_image_push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

type pushClient struct {
	client.APIClient
	images      map[string]client.ImageInspectResult
	stream      string
	pushErr     error
	pushed      []string
	pushOptions []client.ImagePushOptions
}

type pushStream struct{ io.ReadCloser }

func (pushStream) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}
func (pushStream) Wait(context.Context) error { return nil }

func (fake *pushClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: image not found", errdefs.ErrNotFound)
	}
	return image, nil
}

func (fake *pushClient) ImagePush(_ context.Context, reference string, options client.ImagePushOptions) (client.ImagePushResponse, error) {
	fake.pushed = append(fake.pushed, reference)
	fake.pushOptions = append(fake.pushOptions, options)
	if fake.pushErr != nil {
		return nil, fake.pushErr
	}
	return pushStream{io.NopCloser(bytes.NewBufferString(fake.stream))}, nil
}

func (*pushClient) Close() error { return nil }

func pushInspect(id string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID: id, Architecture: "amd64", Os: "linux",
	}}
}

func pushDependencies(fake *pushClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestPushReturnsRawImageAndDerivesChangedFromProgress(t *testing.T) {
	reference := "registry.example.test:5000/team/app:v1"
	fake := &pushClient{
		images: map[string]client.ImageInspectResult{reference: pushInspect("sha256:first")},
		stream: `{"status":"Pushing","id":"layer"}` + "\n" +
			`{"status":"Pushed","id":"layer"}` + "\n",
	}
	response := ExecuteWithDependencies(Request{Name: "registry.example.test:5000/team/app", Tag: "v1"}, pushDependencies(fake))
	if response.Failed || !response.Changed || response.Image["Id"] != "sha256:first" {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.pushed) != 1 || fake.pushed[0] != reference ||
		len(response.Actions) != 1 || response.Actions[0] != "Pushed image "+reference {
		t.Fatalf("pushes/actions = %#v/%#v", fake.pushed, response.Actions)
	}
	decoded, err := base64.URLEncoding.DecodeString(fake.pushOptions[0].RegistryAuth)
	if err != nil || string(decoded) != "{}" {
		t.Fatalf("anonymous RegistryAuth = %q, %v", decoded, err)
	}

	fake.stream = `{"status":"Layer already exists","id":"layer"}` + "\n" +
		`{"status":"v1: digest: sha256:abc size: 1"}` + "\n"
	response = ExecuteWithDependencies(Request{Name: reference}, pushDependencies(fake))
	if response.Failed || response.Changed || response.Image["Id"] != "sha256:first" {
		t.Fatalf("idempotent response = %#v", response)
	}
}

func TestPushReadsRegistryAuthenticationFromDockerConfig(t *testing.T) {
	directory := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte("testuser:hunter2"))
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(
		`{"auths":{"registry.example.test:5000":{"auth":"`+encoded+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reference := "registry.example.test:5000/team/app:latest"
	fake := &pushClient{
		images: map[string]client.ImageInspectResult{reference: pushInspect("sha256:first")},
		stream: `{"status":"Pushed","id":"layer"}` + "\n",
	}
	dependencies := pushDependencies(fake)
	dependencies.Environment = docker.StaticEnvironment{"DOCKER_CONFIG": directory}
	response := ExecuteWithDependencies(Request{Name: "registry.example.test:5000/team/app"}, dependencies)
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	raw, err := base64.URLEncoding.DecodeString(fake.pushOptions[0].RegistryAuth)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Username != "testuser" || auth.Password != "hunter2" {
		t.Fatalf("auth = %#v", auth)
	}
}

func TestPushValidationMatchesUpstreamFailures(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		request Request
		message string
	}{
		{Request{Name: strings.Repeat("a", 12)}, "Cannot push an image by ID"},
		{Request{Name: "example/app@" + digest}, "Cannot push an image by digest"},
		{Request{Name: "example/app", Tag: "foo/bar"}, `"foo/bar" is not a valid docker tag!`},
		{Request{Name: "example/app:foo bar"}, `"foo bar" is not a valid docker tag!`},
	} {
		response := ExecuteWithDependencies(test.request, docker.Dependencies{})
		if !response.Failed || response.Msg != test.message {
			t.Fatalf("%#v returned %#v", test.request, response)
		}
	}

	response := ExecuteWithDependencies(Request{Name: "registry.example.test:5000/team/missing"}, pushDependencies(&pushClient{
		images: map[string]client.ImageInspectResult{},
	}))
	if !response.Failed || response.Msg != "Cannot find image registry.example.test:5000/team/missing:latest" {
		t.Fatalf("missing response = %#v", response)
	}

	emptyTag := Request{Name: "example/app"}
	emptyTag.SetProvidedArguments([]string{"name", "tag"})
	response = ExecuteWithDependencies(emptyTag, docker.Dependencies{})
	if !response.Failed || response.Msg != `"" is not a valid docker tag!` {
		t.Fatalf("empty tag response = %#v", response)
	}
}

func TestPushSurfacesEmbeddedAndAuthorizationErrors(t *testing.T) {
	reference := "registry.example.test:5000/team/app:latest"
	fake := &pushClient{
		images: map[string]client.ImageInspectResult{reference: pushInspect("sha256:first")},
		stream: `{"errorDetail":{"message":"blob upload invalid"}}` + "\n",
	}
	response := ExecuteWithDependencies(Request{Name: reference}, pushDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "blob upload invalid") {
		t.Fatalf("stream response = %#v", response)
	}

	fake.stream = `{"errorDetail":{"message":"unauthorized: authentication required"}}` + "\n"
	response = ExecuteWithDependencies(Request{Name: reference}, pushDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "Try logging into registry.example.test:5000 first") {
		t.Fatalf("auth response = %#v", response)
	}

	fake.pushErr = fmt.Errorf("unauthorized: access denied")
	response = ExecuteWithDependencies(Request{Name: reference}, pushDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "Does the repository exist?") {
		t.Fatalf("repository response = %#v", response)
	}
}
