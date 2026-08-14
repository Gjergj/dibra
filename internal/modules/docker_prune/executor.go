package docker_prune

import (
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
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	response := Response{}
	changed := false

	if req.Containers {
		result, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: req.ContainersFilters.ToClientFilters()})
		if err != nil {
			return failedResponse(docker.WrapError("prune containers", "", err).Error())
		}
		deleted := result.Report.ContainersDeleted
		if deleted == nil {
			deleted = []string{}
		}
		response.Containers = &deleted
		reclaimed := result.Report.SpaceReclaimed
		response.ContainersSpaceReclaimed = &reclaimed
		if len(deleted) > 0 || reclaimed > 0 {
			changed = true
		}
	}

	if req.Images {
		result, err := cli.ImagePrune(ctx, client.ImagePruneOptions{Filters: req.ImagesFilters.ToClientFilters()})
		if err != nil {
			return failedResponse(docker.WrapError("prune images", "", err).Error())
		}
		images, err := docker.InspectionSlice(result.Report.ImagesDeleted)
		if err != nil {
			return failedResponse(fmt.Sprintf("encode pruned images: %v", err))
		}
		response.Images = &images
		reclaimed := result.Report.SpaceReclaimed
		response.ImagesSpaceReclaimed = &reclaimed
		if len(images) > 0 || reclaimed > 0 {
			changed = true
		}
	}

	if req.Networks {
		result, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: req.NetworksFilters.ToClientFilters()})
		if err != nil {
			return failedResponse(docker.WrapError("prune networks", "", err).Error())
		}
		deleted := result.Report.NetworksDeleted
		if deleted == nil {
			deleted = []string{}
		}
		response.Networks = &deleted
		if len(deleted) > 0 {
			changed = true
		}
	}

	if req.Volumes {
		result, err := cli.VolumePrune(ctx, client.VolumePruneOptions{Filters: req.VolumesFilters.ToClientFilters()})
		if err != nil {
			return failedResponse(docker.WrapError("prune volumes", "", err).Error())
		}
		deleted := result.Report.VolumesDeleted
		if deleted == nil {
			deleted = []string{}
		}
		response.Volumes = &deleted
		reclaimed := result.Report.SpaceReclaimed
		response.VolumesSpaceReclaimed = &reclaimed
		if len(deleted) > 0 || reclaimed > 0 {
			changed = true
		}
	}

	if req.pruneBuilderCache() {
		options := client.BuildCachePruneOptions{
			All:     req.BuilderCacheAll,
			Filters: req.BuilderCacheFilters.ToClientFilters(),
		}
		if req.BuilderCacheKeepStorage != "" {
			reserved, err := docker.ParseHumanBytes(req.BuilderCacheKeepStorage)
			if err != nil {
				return failedResponse(err.Error())
			}
			options.ReservedSpace = reserved
		}
		result, err := cli.BuildCachePrune(ctx, options)
		if err != nil {
			return failedResponse(docker.WrapError("prune builder cache", "", err).Error())
		}
		reclaimed := result.Report.SpaceReclaimed
		response.BuilderCacheSpaceReclaimed = &reclaimed
		deleted := result.Report.CachesDeleted
		if deleted == nil {
			deleted = []string{}
		}
		response.BuilderCacheCachesDeleted = &deleted
		if reclaimed > 0 || len(deleted) > 0 {
			changed = true
		}
	}

	response.Changed = changed
	return response
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
