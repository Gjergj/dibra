package docker_prune

import (
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()
	resp := Response{}
	var reclaimed uint64

	// Prune Containers
	if req.Containers {
		f := buildFilters(req.ContainersFilters)
		pruneReport, err := cli.ContainersPrune(ctx, f)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune containers", "", err).Error()}
		}
		if len(pruneReport.ContainersDeleted) > 0 {
			resp.Changed = true
			resp.ContainersDeleted = pruneReport.ContainersDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Images
	if req.Images {
		f := buildFilters(req.ImagesFilters)
		pruneReport, err := cli.ImagesPrune(ctx, f)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune images", "", err).Error()}
		}
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
		pruneReport, err := cli.NetworksPrune(ctx, f)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune networks", "", err).Error()}
		}
		if len(pruneReport.NetworksDeleted) > 0 {
			resp.Changed = true
			resp.NetworksDeleted = pruneReport.NetworksDeleted
		}
	}

	// Prune Volumes
	if req.Volumes {
		f := buildFilters(req.VolumesFilters)
		pruneReport, err := cli.VolumesPrune(ctx, f)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("prune volumes", "", err).Error()}
		}
		if len(pruneReport.VolumesDeleted) > 0 {
			resp.Changed = true
			resp.VolumesDeleted = pruneReport.VolumesDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Builder
	if req.Builder {
		opts := types.BuildCachePruneOptions{
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
		if pruneReport.SpaceReclaimed > 0 || len(pruneReport.CachesDeleted) > 0 {
			resp.Changed = true
			resp.BuildCacheItemsUsed = len(pruneReport.CachesDeleted)
			reclaimed += pruneReport.SpaceReclaimed
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
func buildFilters(filterMap map[string]string) filters.Args {
	f := filters.NewArgs()
	for k, v := range filterMap {
		f.Add(k, v)
	}
	return f
}
