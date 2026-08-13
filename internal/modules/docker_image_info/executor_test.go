package docker_image_info

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type infoClient struct {
	client.APIClient
	images      map[string]client.ImageInspectResult
	inspectErrs map[string]error
	list        client.ImageListResult
	listErr     error
	inspected   []string
}

func (fake *infoClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	fake.inspected = append(fake.inspected, reference)
	if err := fake.inspectErrs[reference]; err != nil {
		return client.ImageInspectResult{}, err
	}
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: image not found", errdefs.ErrNotFound)
	}
	return image, nil
}

func (fake *infoClient) ImageList(context.Context, client.ImageListOptions) (client.ImageListResult, error) {
	return fake.list, fake.listErr
}

func (*infoClient) Close() error { return nil }

func infoInspect(id string, tags ...string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID:           id,
		RepoTags:     tags,
		Architecture: "amd64",
		Os:           "linux",
	}}
}

func infoDependencies(fake *infoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestSelectedImagesPreserveOrderDuplicatesAndSkipMissing(t *testing.T) {
	alpine := infoInspect("sha256:aaaaaaaaaaaaaaaa", "alpine:latest")
	busybox := infoInspect("sha256:bbbbbbbbbbbbbbbb", "busybox:v1")
	fake := &infoClient{images: map[string]client.ImageInspectResult{
		"alpine:latest":           alpine,
		"busybox:v1":              busybox,
		"aaaaaaaaaaaa":            alpine,
		"sha256:aaaaaaaaaaaaaaaa": alpine,
	}}
	response := ExecuteWithDependencies(Request{Name: StringList{
		"missing", "busybox:v1", "alpine", "aaaaaaaaaaaa", "alpine",
	}}, infoDependencies(fake))

	if response.Failed || response.Changed || len(response.Images) != 4 {
		t.Fatalf("response = %#v", response)
	}
	wantIDs := []any{"sha256:bbbbbbbbbbbbbbbb", "sha256:aaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaa", "sha256:aaaaaaaaaaaaaaaa"}
	for index, want := range wantIDs {
		if response.Images[index]["Id"] != want {
			t.Fatalf("images[%d].Id = %#v, want %#v", index, response.Images[index]["Id"], want)
		}
	}
	if response.Images[0]["Architecture"] != "amd64" || response.Images[0]["Os"] != "linux" {
		t.Fatalf("raw inspection fields missing: %#v", response.Images[0])
	}
}

func TestNoNamesListsAndFullyInspectsAllImages(t *testing.T) {
	first := infoInspect("sha256:first", "first:latest")
	second := infoInspect("sha256:second", "second:latest")
	fake := &infoClient{
		images: map[string]client.ImageInspectResult{
			"sha256:first": first,
		},
		inspectErrs: map[string]error{
			"sha256:gone": fmt.Errorf("%w: disappeared", errdefs.ErrNotFound),
		},
		list: client.ImageListResult{Items: []imagetypes.Summary{
			{ID: "sha256:first"},
			{ID: "sha256:gone"},
			{ID: "sha256:second"},
		}},
	}
	fake.images["sha256:second"] = second

	response := ExecuteWithDependencies(Request{}, infoDependencies(fake))
	if response.Failed || len(response.Images) != 3 || response.Images[0]["Id"] != "sha256:first" ||
		response.Images[1] != nil || response.Images[2]["Id"] != "sha256:second" {
		t.Fatalf("response = %#v", response)
	}
}

func TestUnexpectedListAndInspectErrorsFail(t *testing.T) {
	listFailure := &infoClient{listErr: errors.New("daemon unavailable")}
	response := ExecuteWithDependencies(Request{}, infoDependencies(listFailure))
	if !response.Failed || !strings.Contains(response.Msg, "list images") {
		t.Fatalf("list response = %#v", response)
	}

	inspectFailure := &infoClient{inspectErrs: map[string]error{"alpine:latest": errors.New("permission denied")}}
	response = ExecuteWithDependencies(Request{Name: StringList{"alpine"}}, infoDependencies(inspectFailure))
	if !response.Failed || !strings.Contains(response.Msg, "inspect image") {
		t.Fatalf("inspect response = %#v", response)
	}
}

func TestStringListAcceptsScalarAndList(t *testing.T) {
	for input, count := range map[string]int{`"alpine"`: 1, `["alpine","busybox"]`: 2} {
		var values StringList
		if err := values.UnmarshalJSON([]byte(input)); err != nil || len(values) != count {
			t.Fatalf("UnmarshalJSON(%s) = %#v, %v", input, values, err)
		}
	}
}
