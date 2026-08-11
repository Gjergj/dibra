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
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()
	resp := Response{}
	var reclaimed uint64

	// Prune Containers
	if req.Containers {
		f := buildFilters(req.ContainersFilters)
		result, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: f})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune containers", "", err).Error()}
		}
		pruneReport := result.Report
		if len(pruneReport.ContainersDeleted) > 0 {
			resp.Changed = true
			resp.ContainersDeleted = pruneReport.ContainersDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Images
	if req.Images {
		f := buildFilters(req.ImagesFilters)
		result, err := cli.ImagePrune(ctx, client.ImagePruneOptions{Filters: f})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune images", "", err).Error()}
		}
		pruneReport := result.Report
		if len(pruneReport.ImagesDeleted) > 0 {
			resp.Changed = true
			for _, img := range pruneReport.ImagesDeleted {
				if img.Deleted != "" {
					resp.ImagesDeleted = append(resp.ImagesDeleted, img.Deleted)
				}
			}
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Networks
	if req.Networks {
		f := buildFilters(req.NetworksFilters)
		result, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{Filters: f})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune networks", "", err).Error()}
		}
		pruneReport := result.Report
		if len(pruneReport.NetworksDeleted) > 0 {
			resp.Changed = true
			resp.NetworksDeleted = pruneReport.NetworksDeleted
		}
	}

	// Prune Volumes
	if req.Volumes {
		f := buildFilters(req.VolumesFilters)
		result, err := cli.VolumePrune(ctx, client.VolumePruneOptions{Filters: f})
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune volumes", "", err).Error()}
		}
		pruneReport := result.Report
		if len(pruneReport.VolumesDeleted) > 0 {
			resp.Changed = true
			resp.VolumesDeleted = pruneReport.VolumesDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Builder
	if req.Builder {
		opts := client.BuildCachePruneOptions{
			All: req.BuilderCacheAll,
		}
		if req.BuilderCacheFilters != nil {
			opts.Filters = buildFilters(req.BuilderCacheFilters)
		}
		pruneReport, err := cli.BuildCachePrune(ctx, opts)
		if err != nil {
			// Some older daemons may not support this; treat as soft error
			return Response{Failed: true, Msg: docker.WrapError("prune builder cache", "", err).Error()}
		}
		report := pruneReport.Report
		if report.SpaceReclaimed > 0 || len(report.CachesDeleted) > 0 {
			resp.Changed = true
			resp.BuildCacheItemsUsed = len(report.CachesDeleted)
			reclaimed += report.SpaceReclaimed
		}
	}

	resp.SpaceReclaimed = reclaimed
	if resp.Changed {
		resp.Msg = "Pruned requested resources"
	} else {
		resp.Msg = "Nothing to prune"
	}

	return resp
}

// buildFilters converts a map of filter key-value pairs to Docker filters.Args
func buildFilters(filterMap map[string]string) client.Filters {
	f := client.Filters{}
	for k, v := range filterMap {
		f.Add(k, v)
	}
	return f
}
