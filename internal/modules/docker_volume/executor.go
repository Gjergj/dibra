package docker_volume

import (
	"fmt"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
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

	state := req.State
	if state == "" {
		state = "present"
	}

	// Helper to inspect volume
	findVolume := func(name string) (volume.Volume, bool, error) {
		vol, err := cli.VolumeInspect(ctx, name)
		if err != nil {
			if client.IsErrNotFound(err) {
				return volume.Volume{}, false, nil
			}
			return volume.Volume{}, false, err
		}
		return vol, true, nil
	}

	existing, exists, err := findVolume(req.Name)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect volume: %v", err)}
	}

	if state == "absent" {
		if !exists {
			return Response{Changed: false, Msg: "volume already absent"}
		}
		// Remove
		err := cli.VolumeRemove(ctx, req.Name, req.Force)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove volume: %v", err)}
		}
		return Response{Changed: true, Msg: "volume removed", VolumeID: existing.Name} // ID is often same as name for volumes
	}

	if state == "present" {
		if exists {
			// Idempotency check
			if req.Recreate == "always" {
				// Remove and recreate
				err := cli.VolumeRemove(ctx, req.Name, req.Force)
				if err != nil {
					return Response{Failed: true, Msg: fmt.Sprintf("failed to remove volume for recreation: %v", err)}
				}
			} else {
				// Check if properties match?
				// Driver check
				if req.Driver != "" && existing.Driver != req.Driver {
					return Response{Failed: true, Msg: fmt.Sprintf("volume exists with different driver: %s != %s", existing.Driver, req.Driver)}
				}
				// TODO: Check labels/options if strict
				return Response{Changed: false, Msg: "volume already exists", VolumeID: existing.Name}
			}
		}

		// Create
		opts := volume.CreateOptions{
			Name:       req.Name,
			Driver:     req.Driver,
			DriverOpts: req.DriverOptions,
			Labels:     req.Labels,
		}

		resp, err := cli.VolumeCreate(ctx, opts)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create volume: %v", err)}
		}

		return Response{Changed: true, Msg: "volume created", VolumeID: resp.Name}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
