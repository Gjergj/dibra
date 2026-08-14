package docker_host_info

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type hostInfoClient struct {
	client.APIClient
	info         system.Info
	infoErr      error
	diskUsage    client.DiskUsageResult
	containers   []container.Summary
	images       []image.Summary
	networks     []network.Summary
	volumes      []volume.Volume
	listAll      bool
	imageFilters client.Filters
}

func (fake *hostInfoClient) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{Info: fake.info}, fake.infoErr
}
func (fake *hostInfoClient) DiskUsage(context.Context, client.DiskUsageOptions) (client.DiskUsageResult, error) {
	return fake.diskUsage, nil
}
func (fake *hostInfoClient) ContainerList(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	fake.listAll = options.All
	return client.ContainerListResult{Items: fake.containers}, nil
}
func (fake *hostInfoClient) ImageList(_ context.Context, options client.ImageListOptions) (client.ImageListResult, error) {
	fake.imageFilters = options.Filters
	return client.ImageListResult{Items: fake.images}, nil
}
func (fake *hostInfoClient) NetworkList(context.Context, client.NetworkListOptions) (client.NetworkListResult, error) {
	return client.NetworkListResult{Items: fake.networks}, nil
}
func (fake *hostInfoClient) VolumeList(context.Context, client.VolumeListOptions) (client.VolumeListResult, error) {
	return client.VolumeListResult{Items: fake.volumes}, nil
}
func (*hostInfoClient) Close() error { return nil }

func hostInfoDependencies(fake *hostInfoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestHostInfoReturnsRawEngineFields(t *testing.T) {
	fake := &hostInfoClient{info: system.Info{ID: "abc", Name: "testhost", ServerVersion: "29.7.2", NCPU: 4}}
	response := ExecuteWithDependencies(Request{}, hostInfoDependencies(fake))
	if response.Failed || !response.CanTalkToDocker || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if response.HostInfo["Name"] != "testhost" || response.HostInfo["ID"] != "abc" || response.HostInfo["NCPU"] != float64(4) {
		t.Fatalf("host_info = %#v", response.HostInfo)
	}
	if _, found := response.HostInfo["ncpu"]; found {
		t.Fatalf("snake-case leaked: %#v", response.HostInfo)
	}
	if response.Containers != nil || response.DiskUsage != nil {
		t.Fatalf("unrequested groups present: %#v", response)
	}
}

func TestHostInfoConnectionFailureSetsCanTalkToDockerFalse(t *testing.T) {
	dependencies := docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return nil, errors.New("dial unix:///bad.sock")
		},
	}
	response := ExecuteWithDependencies(Request{}, dependencies)
	if !response.Failed || response.CanTalkToDocker {
		t.Fatalf("response = %#v", response)
	}
}

func TestHostInfoListsProjectedAndVerboseObjects(t *testing.T) {
	fake := &hostInfoClient{
		info:       system.Info{Name: "host"},
		containers: []container.Summary{{ID: "c1", Image: "alpine", Command: "sleep", Status: "Up", Names: []string{"/web"}}},
		images:     []image.Summary{{ID: "i1", RepoTags: []string{"alpine:latest"}, Created: 1, Size: 2, ParentID: "p1"}},
		networks:   []network.Summary{{Network: network.Network{ID: "n1", Name: "bridge", Driver: "bridge", Scope: "local"}}},
		volumes:    []volume.Volume{{Name: "data", Driver: "local", Mountpoint: "/var/lib/docker/volumes/data"}},
	}
	response := ExecuteWithDependencies(Request{
		Containers: true, Images: true, Networks: true, Volumes: true,
		ImagesFilters: docker.FilterMap{"dangling": {"true"}},
	}, hostInfoDependencies(fake))
	if response.Failed || response.Containers == nil || len(*response.Containers) != 1 {
		t.Fatalf("response = %#v", response)
	}
	container := (*response.Containers)[0]
	if container["Id"] != "c1" || container["Image"] != "alpine" {
		t.Fatalf("container = %#v", container)
	}
	if _, found := container["HostConfig"]; found {
		t.Fatalf("non-verbose container leaked extra keys: %#v", container)
	}
	imageRecord := (*response.Images)[0]
	if imageRecord["Id"] != "i1" {
		t.Fatalf("image = %#v", imageRecord)
	}
	if _, found := imageRecord["ParentId"]; found {
		t.Fatalf("non-verbose image leaked ParentId: %#v", imageRecord)
	}
	if !fake.imageFilters["dangling"]["true"] {
		t.Fatalf("image filters = %#v", fake.imageFilters)
	}

	verbose := ExecuteWithDependencies(Request{Images: true, VerboseOutput: true}, hostInfoDependencies(fake))
	if verbose.Images == nil || (*verbose.Images)[0]["ParentId"] != "p1" {
		t.Fatalf("verbose images = %#v", verbose.Images)
	}
}

func TestHostInfoDiskUsageNonVerboseIsLayersSizeOnly(t *testing.T) {
	fake := &hostInfoClient{
		info: system.Info{Name: "host"},
		diskUsage: client.DiskUsageResult{
			Images: client.ImagesDiskUsage{
				TotalSize: 99,
				Items:     []image.Summary{{ID: "i1"}},
			},
			Containers: client.ContainersDiskUsage{Items: []container.Summary{{ID: "c1"}}},
		},
	}
	response := ExecuteWithDependencies(Request{DiskUsage: true}, hostInfoDependencies(fake))
	if response.Failed || response.DiskUsage["LayersSize"] != int64(99) {
		t.Fatalf("disk_usage = %#v", response.DiskUsage)
	}
	if _, found := response.DiskUsage["Images"]; found {
		t.Fatalf("non-verbose disk usage leaked Images: %#v", response.DiskUsage)
	}

	verbose := ExecuteWithDependencies(Request{DiskUsage: true, VerboseOutput: true}, hostInfoDependencies(fake))
	images, _ := verbose.DiskUsage["Images"].([]map[string]any)
	if len(images) != 1 || images[0]["Id"] != "i1" {
		t.Fatalf("verbose disk usage = %#v", verbose.DiskUsage)
	}
}

func TestHostInfoContainersAllIsForwarded(t *testing.T) {
	fake := &hostInfoClient{info: system.Info{Name: "host"}}
	_ = ExecuteWithDependencies(Request{Containers: true, ContainersAll: true}, hostInfoDependencies(fake))
	if !fake.listAll {
		t.Fatal("expected containers_all to set All")
	}
}

func TestHostInfoEmptyListsArePresentWhenRequested(t *testing.T) {
	fake := &hostInfoClient{info: system.Info{Name: "host"}}
	response := ExecuteWithDependencies(Request{Networks: true}, hostInfoDependencies(fake))
	if response.Networks == nil || !reflect.DeepEqual(*response.Networks, []map[string]any{}) {
		t.Fatalf("networks = %#v", response.Networks)
	}
}
