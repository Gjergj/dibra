package docker_host_info

import (
	"context"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, CanTalkToDocker: false, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Response{Failed: true, CanTalkToDocker: false, Msg: docker.WrapError("inspect docker host", "", err).Error()}
	}
	hostInfo, err := docker.InspectionMap(infoResult.Info)
	if err != nil {
		return Response{Failed: true, CanTalkToDocker: false, Msg: fmt.Sprintf("encode docker host info: %v", err)}
	}

	response := Response{CanTalkToDocker: true, HostInfo: hostInfo}

	if req.DiskUsage {
		usage, err := diskUsageFacts(ctx, cli, req.VerboseOutput)
		if err != nil {
			return Response{Failed: true, CanTalkToDocker: true, Msg: docker.WrapError("inspect docker host", "", err).Error()}
		}
		response.DiskUsage = usage
	}

	if req.Containers {
		items, err := listContainers(ctx, cli, req)
		if err != nil {
			return Response{Failed: true, CanTalkToDocker: true, Msg: docker.WrapError("inspect docker host for object 'containers'", "", err).Error()}
		}
		response.Containers = &items
	}
	if req.Images {
		items, err := listImages(ctx, cli, req)
		if err != nil {
			return Response{Failed: true, CanTalkToDocker: true, Msg: docker.WrapError("inspect docker host for object 'images'", "", err).Error()}
		}
		response.Images = &items
	}
	if req.Networks {
		items, err := listNetworks(ctx, cli, req)
		if err != nil {
			return Response{Failed: true, CanTalkToDocker: true, Msg: docker.WrapError("inspect docker host for object 'networks'", "", err).Error()}
		}
		response.Networks = &items
	}
	if req.Volumes {
		items, err := listVolumes(ctx, cli, req)
		if err != nil {
			return Response{Failed: true, CanTalkToDocker: true, Msg: docker.WrapError("inspect docker host for object 'volumes'", "", err).Error()}
		}
		response.Volumes = &items
	}

	return response
}

func diskUsageFacts(ctx context.Context, cli client.APIClient, verbose bool) (map[string]any, error) {
	result, err := cli.DiskUsage(ctx, client.DiskUsageOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		BuildCache: true,
		Verbose:    true,
	})
	if err != nil {
		return nil, err
	}
	usage := map[string]any{"LayersSize": result.Images.TotalSize}
	if !verbose {
		return usage, nil
	}
	images, err := docker.InspectionSlice(result.Images.Items)
	if err != nil {
		return nil, err
	}
	containers, err := docker.InspectionSlice(result.Containers.Items)
	if err != nil {
		return nil, err
	}
	volumes, err := docker.InspectionSlice(result.Volumes.Items)
	if err != nil {
		return nil, err
	}
	buildCache, err := docker.InspectionSlice(result.BuildCache.Items)
	if err != nil {
		return nil, err
	}
	usage["Images"] = images
	usage["Containers"] = containers
	usage["Volumes"] = volumes
	usage["BuildCache"] = buildCache
	return usage, nil
}

func listContainers(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     req.ContainersAll,
		Filters: req.ContainersFilters.ToClientFilters(),
	})
	if err != nil {
		return nil, err
	}
	return projectItems(result.Items, req.VerboseOutput, []string{"Id", "Image", "Command", "Created", "Status", "Ports", "Names"})
}

func listImages(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.ImageList(ctx, client.ImageListOptions{Filters: req.ImagesFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	return projectItems(result.Items, req.VerboseOutput, []string{"Id", "RepoTags", "Created", "Size"})
}

func listNetworks(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.NetworkList(ctx, client.NetworkListOptions{Filters: req.NetworksFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	return projectItems(result.Items, req.VerboseOutput, []string{"Id", "Driver", "Name", "Scope"})
}

func listVolumes(ctx context.Context, cli client.APIClient, req Request) ([]map[string]any, error) {
	result, err := cli.VolumeList(ctx, client.VolumeListOptions{Filters: req.VolumesFilters.ToClientFilters()})
	if err != nil {
		return nil, err
	}
	return projectItems(result.Items, req.VerboseOutput, []string{"Driver", "Name"})
}

func projectItems(value any, verbose bool, keys []string) ([]map[string]any, error) {
	items, err := docker.InspectionSlice(value)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []map[string]any{}
	}
	if verbose {
		return items, nil
	}
	projected := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record := map[string]any{}
		for _, key := range keys {
			record[key] = item[key]
		}
		projected = append(projected, record)
	}
	return projected, nil
}
