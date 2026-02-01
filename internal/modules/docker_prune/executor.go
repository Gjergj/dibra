package docker_prune

import (
	"context"
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

	ctx := context.Background()
	resp := Response{}
	var reclaimed uint64

	// Prune Containers
	if req.Containers {
		pruneReport, err := cli.ContainersPrune(ctx, filters.Args{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to prune containers: %v", err)}
		}
		if len(pruneReport.ContainersDeleted) > 0 {
			resp.Changed = true
			resp.ContainersDeleted = pruneReport.ContainersDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Images
	if req.Images {
		f := filters.NewArgs()
		for k, v := range req.ImagesFilters {
			f.Add(k, v)
		}
		pruneReport, err := cli.ImagesPrune(ctx, f)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to prune images: %v", err)}
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
		pruneReport, err := cli.NetworksPrune(ctx, filters.Args{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to prune networks: %v", err)}
		}
		if len(pruneReport.NetworksDeleted) > 0 {
			resp.Changed = true
			resp.NetworksDeleted = pruneReport.NetworksDeleted
			// Network prune doesn't report space reclaimed in struct usually, check SDK
		}
	}

	// Prune Volumes
	if req.Volumes {
		pruneReport, err := cli.VolumesPrune(ctx, filters.Args{})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to prune volumes: %v", err)}
		}
		if len(pruneReport.VolumesDeleted) > 0 {
			resp.Changed = true
			resp.VolumesDeleted = pruneReport.VolumesDeleted
			reclaimed += pruneReport.SpaceReclaimed
		}
	}

	// Prune Builder
	if req.Builder {
		pruneReport, err := cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{})
		if err != nil {
			// Some older daemons usually don't support this failing isn't critical maybe?
			// But let's error for now.
			return Response{Failed: true, Msg: fmt.Sprintf("failed to prune builder cache: %v", err)}
		}
		if pruneReport.SpaceReclaimed > 0 {
			resp.Changed = true
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
