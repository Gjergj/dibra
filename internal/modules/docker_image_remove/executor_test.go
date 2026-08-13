package docker_image_remove

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	imagetypes "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

type removeClient struct {
	client.APIClient
	images       map[string]client.ImageInspectResult
	removeResult client.ImageRemoveResult
	removeErr    error
	removed      []string
	options      []client.ImageRemoveOptions
	afterRemove  func(*removeClient)
}

func (fake *removeClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: image not found", errdefs.ErrNotFound)
	}
	return image, nil
}

func (fake *removeClient) ImageRemove(_ context.Context, reference string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	fake.removed = append(fake.removed, reference)
	fake.options = append(fake.options, options)
	if fake.afterRemove != nil {
		fake.afterRemove(fake)
	}
	return fake.removeResult, fake.removeErr
}

func (*removeClient) Close() error { return nil }

func removeInspect(id string, tags, digests []string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID: id, RepoTags: tags, RepoDigests: digests, Architecture: "amd64", Os: "linux",
	}}
}

func removeDependencies(fake *removeClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestMissingRemovalIsIdempotentInCheckAndRealModes(t *testing.T) {
	for _, state := range []execution.State{{CheckMode: true, DiffMode: true}, {DiffMode: true}} {
		fake := &removeClient{images: map[string]client.ImageInspectResult{}}
		response := ExecuteWithDependenciesAndState(Request{Name: "missing"}, removeDependencies(fake), state)
		if response.Failed || response.Changed || len(fake.removed) != 0 ||
			len(response.Deleted) != 0 || len(response.Untagged) != 0 {
			t.Fatalf("state=%#v response=%#v removals=%#v", state, response, fake.removed)
		}
		if response.Diff == nil || response.Diff.Before.Exists || response.Diff.After.Exists {
			t.Fatalf("diff = %#v", response.Diff)
		}
	}
}

func TestCheckModePredictsTagRemovalWithoutMutation(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	image := removeInspect(id,
		[]string{"example:bar", "example:foo", "example:latest"},
		[]string{"example@sha256:" + strings.Repeat("b", 64)})
	fake := &removeClient{images: map[string]client.ImageInspectResult{"example:foo": image}}
	response := ExecuteWithDependenciesAndState(Request{Name: "example:foo"}, removeDependencies(fake),
		execution.State{CheckMode: true, DiffMode: true})

	if response.Failed || !response.Changed || len(fake.removed) != 0 ||
		strings.Join(response.Untagged, ",") != "example:foo" || len(response.Deleted) != 0 {
		t.Fatalf("response = %#v; removals = %#v", response, fake.removed)
	}
	if response.Diff.Before.Tags == nil || response.Diff.After.Tags == nil ||
		strings.Join(*response.Diff.Before.Tags, ",") != "example:bar,example:foo,example:latest" ||
		strings.Join(*response.Diff.After.Tags, ",") != "example:bar,example:latest" {
		t.Fatalf("diff = %#v", response.Diff)
	}
	if response.Image["Id"] != id || response.Actions[0] != "Removed image example:foo" {
		t.Fatalf("raw result = %#v", response)
	}
}

func TestRealRemovalReturnsSortedEngineResultsAndPruneOption(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	image := removeInspect(id, []string{"example:latest"}, nil)
	fake := &removeClient{
		images: map[string]client.ImageInspectResult{"example:latest": image, id: image},
		removeResult: client.ImageRemoveResult{Items: []imagetypes.DeleteResponse{
			{Deleted: "sha256:z"}, {Untagged: "example:latest"}, {Deleted: id},
		}},
		afterRemove: func(fake *removeClient) { delete(fake.images, id) },
	}
	request := Request{Name: "example", Prune: false}
	request.SetProvidedArguments([]string{"name", "prune"})
	response := ExecuteWithDependenciesAndState(request, removeDependencies(fake), execution.State{DiffMode: true})

	if response.Failed || !response.Changed || strings.Join(response.Deleted, ",") != id+",sha256:z" ||
		strings.Join(response.Untagged, ",") != "example:latest" {
		t.Fatalf("response = %#v", response)
	}
	if fake.removed[0] != "example:latest" || fake.options[0].PruneChildren || fake.options[0].Force {
		t.Fatalf("remove call = %#v %#v", fake.removed, fake.options)
	}
	if response.Diff == nil || response.Diff.After.Exists {
		t.Fatalf("diff = %#v", response.Diff)
	}
}

func TestImageIDCheckModeRequiresForceAndPredictsDocker29Contract(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	image := removeInspect(id,
		[]string{"example:latest", "example:v1"},
		[]string{"example@sha256:" + strings.Repeat("b", 64)})
	fake := &removeClient{images: map[string]client.ImageInspectResult{id: image}}

	response := ExecuteWithDependenciesAndState(Request{Name: id}, removeDependencies(fake),
		execution.State{CheckMode: true, DiffMode: true})
	if !response.Failed || response.Msg != "Cannot delete image by ID that is still in use - use force=true" {
		t.Fatalf("non-force response = %#v", response)
	}

	response = ExecuteWithDependenciesAndState(Request{Name: id, Force: true}, removeDependencies(fake),
		execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || len(response.Deleted) != 1 || response.Deleted[0] != id ||
		len(response.Untagged) != 3 || response.Diff == nil || response.Diff.After.Exists {
		t.Fatalf("force response = %#v", response)
	}
}

func TestRemoveValidationAndNotFoundRace(t *testing.T) {
	response := ExecuteWithDependencies(Request{Name: "example", Tag: "foo/bar"}, docker.Dependencies{})
	if !response.Failed || response.Msg != `"foo/bar" is not a valid docker tag` {
		t.Fatalf("validation response = %#v", response)
	}

	id := "sha256:" + strings.Repeat("a", 64)
	image := removeInspect(id, []string{"example:latest"}, nil)
	fake := &removeClient{
		images:    map[string]client.ImageInspectResult{"example:latest": image},
		removeErr: fmt.Errorf("%w: vanished", errdefs.ErrNotFound),
	}
	response = ExecuteWithDependencies(Request{Name: "example"}, removeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("race response = %#v", response)
	}
}
