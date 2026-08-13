package docker_image_pull

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type pullClient struct {
	client.APIClient
	images      map[string]client.ImageInspectResult
	after       map[string]client.ImageInspectResult
	stream      string
	pullErr     error
	pulled      []string
	pullOptions []client.ImagePullOptions
	info        system.Info
}

type pullStream struct{ io.ReadCloser }

func (pullStream) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}
func (pullStream) Wait(context.Context) error { return nil }

func (fake *pullClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: image not found", errdefs.ErrNotFound)
	}
	return image, nil
}

func (fake *pullClient) ImagePull(_ context.Context, reference string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	fake.pulled = append(fake.pulled, reference)
	fake.pullOptions = append(fake.pullOptions, options)
	if fake.pullErr != nil {
		return nil, fake.pullErr
	}
	if image, found := fake.after[reference]; found {
		fake.images[reference] = image
	}
	return pullStream{io.NopCloser(bytes.NewBufferString(fake.stream))}, nil
}

func (fake *pullClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: fake.info}, nil
}

func (*pullClient) Close() error { return nil }

func pullInspect(id, operatingSystem, architecture, variant string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID: id, Os: operatingSystem, Architecture: architecture, Variant: variant,
	}}
}

func pullDependencies(fake *pullClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestPullCheckModePredictsDiffWithoutMutation(t *testing.T) {
	fake := &pullClient{images: map[string]client.ImageInspectResult{}}
	response := ExecuteWithDependenciesAndState(Request{Name: "alpine", Platform: "amd64"},
		pullDependencies(fake), execution.State{CheckMode: true, DiffMode: true})

	if response.Failed || !response.Changed || len(fake.pulled) != 0 {
		t.Fatalf("response = %#v; pulls = %#v", response, fake.pulled)
	}
	if response.Diff.Before.Exists == nil || *response.Diff.Before.Exists || response.Diff.After.ID != "unknown" {
		t.Fatalf("diff = %#v", response.Diff)
	}
	if len(response.Actions) != 1 || response.Actions[0] != "Pulled image alpine:latest" {
		t.Fatalf("actions = %#v", response.Actions)
	}
}

func TestPullAlwaysUsesPlatformAndDetectsChangedByImageID(t *testing.T) {
	before := pullInspect("sha256:old", "linux", "amd64", "")
	after := pullInspect("sha256:new", "linux", "amd64", "")
	fake := &pullClient{
		images: map[string]client.ImageInspectResult{"alpine:latest": before},
		after:  map[string]client.ImageInspectResult{"alpine:latest": after},
		stream: `{"status":"Pulling","id":"layer"}` + "\n",
		info:   system.Info{OSType: "linux", Architecture: "amd64"},
	}
	response := ExecuteWithDependencies(Request{Name: "alpine", Platform: "amd64"}, pullDependencies(fake))

	if response.Failed || !response.Changed || response.Image["Id"] != "sha256:new" {
		t.Fatalf("response = %#v", response)
	}
	if len(fake.pullOptions) != 1 || len(fake.pullOptions[0].Platforms) != 1 ||
		fake.pullOptions[0].Platforms[0].OS != "linux" || fake.pullOptions[0].Platforms[0].Architecture != "amd64" {
		t.Fatalf("pull options = %#v", fake.pullOptions)
	}
	if response.Diff.Before.ID != "sha256:old" || response.Diff.After.ID != "sha256:new" {
		t.Fatalf("diff = %#v", response.Diff)
	}

	fake.after["alpine:latest"] = after
	response = ExecuteWithDependencies(Request{Name: "alpine:latest"}, pullDependencies(fake))
	if response.Failed || response.Changed || len(response.Actions) != 1 {
		t.Fatalf("idempotent response = %#v", response)
	}
}

func TestNotPresentSkipsMatchingPlatformAndPullsMismatch(t *testing.T) {
	image := pullInspect("sha256:one", "linux", "amd64", "")
	fake := &pullClient{
		images: map[string]client.ImageInspectResult{"alpine:latest": image},
		after:  map[string]client.ImageInspectResult{"alpine:latest": pullInspect("sha256:two", "linux", "arm64", "v8")},
		stream: `{"status":"Pulled","id":"layer"}` + "\n",
		info:   system.Info{OSType: "linux", Architecture: "amd64"},
	}
	response := ExecuteWithDependencies(Request{Name: "alpine", Pull: "not_present", Platform: "linux/amd64"}, pullDependencies(fake))
	if response.Failed || response.Changed || len(response.Actions) != 0 || len(fake.pulled) != 0 {
		t.Fatalf("matching response = %#v; pulls = %#v", response, fake.pulled)
	}

	response = ExecuteWithDependencies(Request{Name: "alpine", Pull: "not_present", Platform: "linux/arm64"}, pullDependencies(fake))
	if response.Failed || !response.Changed || len(fake.pulled) != 1 {
		t.Fatalf("mismatch response = %#v; pulls = %#v", response, fake.pulled)
	}
}

func TestDigestAndEmbeddedTagTakePrecedence(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	fake := &pullClient{
		images: map[string]client.ImageInspectResult{},
		after: map[string]client.ImageInspectResult{
			"example/app@" + digest: pullInspect("sha256:digest", "linux", "amd64", ""),
			"example/app:v2":        pullInspect("sha256:tag", "linux", "amd64", ""),
		},
		stream: `{"status":"Downloaded newer image"}` + "\n",
	}
	response := ExecuteWithDependencies(Request{Name: "example/app@" + digest, Tag: "ignored"}, pullDependencies(fake))
	if response.Failed || fake.pulled[0] != "example/app@"+digest ||
		response.Actions[0] != "Pulled image example/app:"+digest {
		t.Fatalf("digest response = %#v; pulls = %#v", response, fake.pulled)
	}
	response = ExecuteWithDependencies(Request{Name: "example/app:v2", Tag: "ignored"}, pullDependencies(fake))
	if response.Failed || fake.pulled[1] != "example/app:v2" {
		t.Fatalf("tag response = %#v; pulls = %#v", response, fake.pulled)
	}
}

func TestExplicitEmptyTagKeepsUpstreamActionAndPullsLatest(t *testing.T) {
	fake := &pullClient{
		images: map[string]client.ImageInspectResult{},
		after:  map[string]client.ImageInspectResult{"alpine:latest": pullInspect("sha256:latest", "linux", "amd64", "")},
		stream: `{"status":"Downloaded newer image"}` + "\n",
	}
	request := Request{Name: "alpine"}
	request.SetProvidedArguments([]string{"name", "tag"})
	response := ExecuteWithDependencies(request, pullDependencies(fake))
	if response.Failed || fake.pulled[0] != "alpine:latest" || response.Actions[0] != "Pulled image alpine:" {
		t.Fatalf("response = %#v; pulls = %#v", response, fake.pulled)
	}
}

func TestPullValidationVersionAndStreamErrors(t *testing.T) {
	for _, test := range []struct {
		request Request
		message string
	}{
		{Request{Name: strings.Repeat("a", 12)}, "Cannot pull an image by ID"},
		{Request{Name: "alpine", Tag: "foo/bar"}, "not a valid docker tag"},
		{Request{Name: "alpine", Pull: "sometimes"}, "always or not_present"},
	} {
		response := ExecuteWithDependencies(test.request, docker.Dependencies{})
		if !response.Failed || !strings.Contains(response.Msg, test.message) {
			t.Fatalf("%#v returned %#v", test.request, response)
		}
	}

	version := "1.31"
	created := false
	response := ExecuteWithDependencies(Request{
		CommonArgs: docker.CommonArgs{APIVersion: &version}, Name: "alpine", Platform: "amd64",
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return nil, nil
		},
	})
	if !response.Failed || !strings.Contains(response.Msg, "1.32") || created {
		t.Fatalf("version response = %#v; created = %v", response, created)
	}

	fake := &pullClient{
		images: map[string]client.ImageInspectResult{},
		stream: `{"errorDetail":{"message":"manifest denied"}}` + "\n",
	}
	response = ExecuteWithDependencies(Request{Name: "alpine"}, pullDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "manifest denied") {
		t.Fatalf("stream response = %#v", response)
	}
}
