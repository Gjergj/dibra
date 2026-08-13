package docker_image_tag

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

type tagClient struct {
	client.APIClient
	images map[string]client.ImageInspectResult
	tagErr error
	tagged []client.ImageTagOptions
}

func (fake *tagClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	image, found := fake.images[reference]
	if !found {
		return client.ImageInspectResult{}, fmt.Errorf("%w: image not found", errdefs.ErrNotFound)
	}
	return image, nil
}

func (fake *tagClient) ImageTag(_ context.Context, options client.ImageTagOptions) (client.ImageTagResult, error) {
	fake.tagged = append(fake.tagged, options)
	if fake.tagErr != nil {
		return client.ImageTagResult{}, fake.tagErr
	}
	fake.images[options.Target] = fake.images[options.Source]
	return client.ImageTagResult{}, nil
}

func (*tagClient) Close() error { return nil }

func tagInspect(id string, tags ...string) client.ImageInspectResult {
	return client.ImageInspectResult{InspectResponse: imagetypes.InspectResponse{
		ID: id, RepoTags: tags, Architecture: "amd64", Os: "linux",
	}}
}

func tagDependencies(fake *tagClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestCheckModePredictsMultipleTagsAndExactDiff(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	source := tagInspect(id, "alpine:latest")
	fake := &tagClient{images: map[string]client.ImageInspectResult{"alpine:latest": source, id: source}}
	response := ExecuteWithDependenciesAndState(Request{
		Name: "alpine", Repository: []string{"example:latest", "example:foo"},
	}, tagDependencies(fake), execution.State{CheckMode: true, DiffMode: true})

	if response.Failed || !response.Changed || len(fake.tagged) != 0 ||
		strings.Join(response.TaggedImages, ",") != "example:latest,example:foo" {
		t.Fatalf("response = %#v; tags = %#v", response, fake.tagged)
	}
	if len(response.Diff.Before.Images) != 2 || len(response.Diff.After.Images) != 2 {
		t.Fatalf("diff = %#v", response.Diff)
	}
	for index := range response.Diff.Before.Images {
		before := response.Diff.Before.Images[index]
		after := response.Diff.After.Images[index]
		if before.Exists == nil || *before.Exists || after.ID != id {
			t.Fatalf("diff[%d] = %#v -> %#v", index, before, after)
		}
	}
	if response.Image["Id"] != id {
		t.Fatalf("raw source inspection = %#v", response.Image)
	}
}

func TestRealTagThenRepeatIsIdempotent(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	source := tagInspect(id, "alpine:latest")
	fake := &tagClient{images: map[string]client.ImageInspectResult{"alpine:latest": source, id: source}}
	request := Request{Name: "alpine:latest", Repository: []string{"example:v1"}}

	first := ExecuteWithDependenciesAndState(request, tagDependencies(fake), execution.State{DiffMode: true})
	if first.Failed || !first.Changed || len(fake.tagged) != 1 ||
		fake.tagged[0].Source != id || fake.tagged[0].Target != "example:v1" {
		t.Fatalf("first = %#v; tags = %#v", first, fake.tagged)
	}
	wantAction := "Tagged image " + id + " as example:v1: target image did not exist"
	if first.Actions[0] != wantAction || first.TaggedImages[0] != "example:v1" {
		t.Fatalf("first actions = %#v/%#v", first.Actions, first.TaggedImages)
	}

	second := ExecuteWithDependenciesAndState(request, tagDependencies(fake),
		execution.State{CheckMode: true, DiffMode: true})
	if second.Failed || second.Changed || len(second.TaggedImages) != 0 ||
		second.Diff.Before.Images[0] != second.Diff.After.Images[0] {
		t.Fatalf("second = %#v", second)
	}
	if !strings.Contains(second.Actions[0], "already exists ("+id+") and is as expected") {
		t.Fatalf("second action = %#v", second.Actions)
	}
}

func TestExistingImageKeepAndOverwrite(t *testing.T) {
	sourceID := "sha256:" + strings.Repeat("a", 64)
	existingID := "sha256:" + strings.Repeat("b", 64)
	source := tagInspect(sourceID, "alpine:latest")
	existing := tagInspect(existingID, "example:latest")
	newFake := func() *tagClient {
		return &tagClient{images: map[string]client.ImageInspectResult{
			"alpine:latest": source, sourceID: source, "example:latest": existing,
		}}
	}

	keepFake := newFake()
	keep := ExecuteWithDependenciesAndState(Request{
		Name: "alpine", Repository: []string{"example"}, ExistingImages: "keep",
	}, tagDependencies(keepFake), execution.State{DiffMode: true})
	if keep.Failed || keep.Changed || len(keepFake.tagged) != 0 ||
		keep.Diff.After.Images[0].ID != existingID ||
		!strings.Contains(keep.Actions[0], "not as expected, but kept") {
		t.Fatalf("keep = %#v; tags = %#v", keep, keepFake.tagged)
	}

	overwriteFake := newFake()
	overwrite := ExecuteWithDependenciesAndState(Request{
		Name: "alpine", Repository: []string{"example"}, ExistingImages: "overwrite",
	}, tagDependencies(overwriteFake), execution.State{CheckMode: true, DiffMode: true})
	if overwrite.Failed || !overwrite.Changed || overwrite.Diff.Before.Images[0].ID != existingID ||
		overwrite.Diff.After.Images[0].ID != sourceID ||
		!strings.Contains(overwrite.Actions[0], "existed ("+existingID+") and was not as expected") {
		t.Fatalf("overwrite = %#v", overwrite)
	}
}

func TestSourceIDDigestAndRepositoryTagDefaults(t *testing.T) {
	id := "sha256:" + strings.Repeat("a", 64)
	digest := "sha256:" + strings.Repeat("b", 64)
	digestReference := "example/source@" + digest
	source := tagInspect(id, "example/source:v2")
	fake := &tagClient{images: map[string]client.ImageInspectResult{
		id: source, digestReference: source,
	}}

	idResult := ExecuteWithDependenciesAndState(Request{
		Name: id, Tag: "foo", Repository: []string{"target"},
	}, tagDependencies(fake), execution.State{CheckMode: true})
	if idResult.Failed || idResult.Diff.After.Images[0].Tag != "foo" ||
		idResult.TaggedImages[0] != "target:foo" {
		t.Fatalf("ID result = %#v", idResult)
	}

	digestResult := ExecuteWithDependenciesAndState(Request{
		Name: digestReference, Repository: []string{"target:v2"},
	}, tagDependencies(fake), execution.State{CheckMode: true})
	if digestResult.Failed || digestResult.Diff.After.Images[0].ID != id {
		t.Fatalf("digest result = %#v", digestResult)
	}
}

func TestTagValidationMissingSourceAndEngineFailure(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		request Request
		message string
	}{
		{Request{Name: "alpine", Tag: "foo/bar", Repository: []string{"target"}}, `"foo/bar" is not a valid docker tag`},
		{Request{Name: "alpine", Repository: []string{digest}}, "repository[1] must not be an image ID"},
		{Request{Name: "alpine", Repository: []string{"target@" + digest}}, "repository[1] must not have a digest"},
		{Request{Name: "alpine", Repository: []string{"target"}, ExistingImages: "invalid"}, "keep or overwrite"},
	} {
		response := ExecuteWithDependencies(test.request, docker.Dependencies{})
		if !response.Failed || !strings.Contains(response.Msg, test.message) {
			t.Fatalf("%#v returned %#v", test.request, response)
		}
	}

	missing := ExecuteWithDependencies(Request{Name: "missing", Repository: []string{"target"}},
		tagDependencies(&tagClient{images: map[string]client.ImageInspectResult{}}))
	if !missing.Failed || missing.Msg != "Cannot find image missing:latest" {
		t.Fatalf("missing = %#v", missing)
	}

	id := "sha256:" + strings.Repeat("c", 64)
	source := tagInspect(id, "alpine:latest")
	fake := &tagClient{
		images: map[string]client.ImageInspectResult{"alpine:latest": source, id: source},
		tagErr: fmt.Errorf("daemon rejected tag"),
	}
	failed := ExecuteWithDependencies(Request{Name: "alpine", Repository: []string{"target"}}, tagDependencies(fake))
	if !failed.Failed || !strings.Contains(failed.Msg, "Error: failed to tag image as target:latest") {
		t.Fatalf("engine failure = %#v", failed)
	}
}
