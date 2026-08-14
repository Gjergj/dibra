package docker_prune

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type pruneClient struct {
	client.APIClient
	containerFilters client.Filters
	imageFilters     client.Filters
	networkFilters   client.Filters
	volumeOptions    client.VolumePruneOptions
	builderOptions   client.BuildCachePruneOptions
	containerReport  container.PruneReport
	imageReport      image.PruneReport
	networkReport    network.PruneReport
	volumeReport     volume.PruneReport
	builderReport    build.CachePruneReport
}

func (fake *pruneClient) ContainerPrune(_ context.Context, options client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
	fake.containerFilters = options.Filters
	return client.ContainerPruneResult{Report: fake.containerReport}, nil
}
func (fake *pruneClient) ImagePrune(_ context.Context, options client.ImagePruneOptions) (client.ImagePruneResult, error) {
	fake.imageFilters = options.Filters
	return client.ImagePruneResult{Report: fake.imageReport}, nil
}
func (fake *pruneClient) NetworkPrune(_ context.Context, options client.NetworkPruneOptions) (client.NetworkPruneResult, error) {
	fake.networkFilters = options.Filters
	return client.NetworkPruneResult{Report: fake.networkReport}, nil
}
func (fake *pruneClient) VolumePrune(_ context.Context, options client.VolumePruneOptions) (client.VolumePruneResult, error) {
	fake.volumeOptions = options
	return client.VolumePruneResult{Report: fake.volumeReport}, nil
}
func (fake *pruneClient) BuildCachePrune(_ context.Context, options client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
	fake.builderOptions = options
	return client.BuildCachePruneResult{Report: fake.builderReport}, nil
}
func (*pruneClient) Close() error { return nil }

func pruneDependencies(fake *pruneClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestPruneReturnsOnlyRequestedGroups(t *testing.T) {
	fake := &pruneClient{
		containerReport: container.PruneReport{ContainersDeleted: []string{"c1"}, SpaceReclaimed: 12},
		imageReport:     image.PruneReport{ImagesDeleted: []image.DeleteResponse{{Deleted: "sha256:abc"}}, SpaceReclaimed: 34},
	}
	response := ExecuteWithDependencies(Request{Containers: true, Images: true}, pruneDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if response.Containers == nil || !reflect.DeepEqual(*response.Containers, []string{"c1"}) {
		t.Fatalf("containers = %#v", response.Containers)
	}
	if response.ContainersSpaceReclaimed == nil || *response.ContainersSpaceReclaimed != 12 {
		t.Fatalf("containers space = %#v", response.ContainersSpaceReclaimed)
	}
	if response.Images == nil || len(*response.Images) != 1 || (*response.Images)[0]["Deleted"] != "sha256:abc" {
		t.Fatalf("images = %#v", response.Images)
	}
	if response.Networks != nil || response.Volumes != nil || response.BuilderCacheSpaceReclaimed != nil {
		t.Fatalf("unexpected groups: %#v", response)
	}
}

func TestPruneEmptyRequestedGroupsStayPresentAndUnchanged(t *testing.T) {
	fake := &pruneClient{}
	response := ExecuteWithDependencies(Request{Networks: true, Volumes: true, BuilderCache: true}, pruneDependencies(fake))
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if response.Networks == nil || len(*response.Networks) != 0 {
		t.Fatalf("networks = %#v", response.Networks)
	}
	if response.Volumes == nil || len(*response.Volumes) != 0 || response.VolumesSpaceReclaimed == nil {
		t.Fatalf("volumes = %#v", response.Volumes)
	}
	if response.BuilderCacheCachesDeleted == nil || len(*response.BuilderCacheCachesDeleted) != 0 {
		t.Fatalf("builder cache = %#v", response.BuilderCacheCachesDeleted)
	}
}

func TestPrunePassesFiltersKeepStorageAndBuilderAlias(t *testing.T) {
	fake := &pruneClient{builderReport: build.CachePruneReport{CachesDeleted: []string{"cache1"}, SpaceReclaimed: 9}}
	request := Request{
		Containers:              true,
		BuilderCache:            true,
		BuilderCacheAll:         true,
		BuilderCacheKeepStorage: "1MB",
		Volumes:                 true,
		VolumesFilters:          docker.FilterMap{"label": {"prune=yes"}, "all": {"true"}},
		ContainersFilters:       docker.FilterMap{"until": {"24h"}, "dangling": {"true"}},
	}
	response := ExecuteWithDependencies(request, pruneDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if !fake.containerFilters["until"]["24h"] || !fake.containerFilters["dangling"]["true"] {
		t.Fatalf("container filters = %#v", fake.containerFilters)
	}
	if !fake.volumeOptions.Filters["all"]["true"] || !fake.volumeOptions.Filters["label"]["prune=yes"] {
		t.Fatalf("volume filters = %#v", fake.volumeOptions.Filters)
	}
	if !fake.builderOptions.All || fake.builderOptions.ReservedSpace != 1024*1024 {
		t.Fatalf("builder options = %#v", fake.builderOptions)
	}
}

func TestPruneKeepStorageParseError(t *testing.T) {
	response := ExecuteWithDependencies(Request{BuilderCache: true, BuilderCacheKeepStorage: "bogus"}, pruneDependencies(&pruneClient{}))
	if !response.Failed || response.Msg == "" {
		t.Fatalf("response = %#v", response)
	}
}

func TestPruneFilterMapAcceptsBooleanJSON(t *testing.T) {
	var request Request
	if err := json.Unmarshal([]byte(`{"images":true,"images_filters":{"dangling":true}}`), &request); err != nil {
		t.Fatal(err)
	}
	if !request.Images || !reflect.DeepEqual([]string(request.ImagesFilters["dangling"]), []string{"true"}) {
		t.Fatalf("request = %#v", request)
	}
}
