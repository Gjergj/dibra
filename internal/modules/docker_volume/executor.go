package docker_volume

import (
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	state := req.State
	if state == "" {
		state = "present"
	}

	// Helper to inspect volume
	findVolume := func(name string) (volume.Volume, bool, error) {
		result, err := cli.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
		if err != nil {
			if docker.IsNotFoundError(err) {
				return volume.Volume{}, false, nil
			}
			return volume.Volume{}, false, err
		}
		return result.Volume, true, nil
	}

	existing, exists, err := findVolume(req.Name)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("inspect volume", req.Name, err).Error()}
	}

	if state == "absent" {
		if !exists {
			return Response{Changed: false, Msg: "volume already absent"}
		}
		// Remove
		_, err := cli.VolumeRemove(ctx, req.Name, client.VolumeRemoveOptions{Force: req.Force})
		if err != nil {
			// Check for "volume in use" error
			if isVolumeInUseError(err) {
				return Response{Failed: true, Msg: fmt.Sprintf("cannot remove volume '%s': volume is in use by a container; use force=true to forcefully remove or stop the container first", req.Name)}
			}
			return Response{Failed: true, Msg: docker.WrapError("remove volume", req.Name, err).Error()}
		}
		return Response{Changed: true, Msg: "volume removed", VolumeID: existing.Name}
	}

	if state == "present" {
		recreate := req.Recreate
		if recreate == "" {
			recreate = "never"
		}

		if exists {
			// Idempotency check
			if recreate == "always" {
				// Remove and recreate
				_, err := cli.VolumeRemove(ctx, req.Name, client.VolumeRemoveOptions{Force: req.Force})
				if err != nil {
					if isVolumeInUseError(err) {
						return Response{Failed: true, Msg: fmt.Sprintf("cannot recreate volume '%s': volume is in use by a container; use force=true or stop the container first", req.Name)}
					}
					return Response{Failed: true, Msg: docker.WrapError("remove volume for recreation", req.Name, err).Error()}
				}
			} else {
				// Check if properties match
				if req.Driver != "" && existing.Driver != req.Driver {
					return Response{Failed: true, Msg: fmt.Sprintf("volume exists with different driver: %s != %s; use recreate=always to recreate", existing.Driver, req.Driver)}
				}

				// Deep compare driver options
				if req.DriverOptions != nil && !docker.CompareMaps(existing.Options, req.DriverOptions) {
					return Response{Failed: true, Msg: "volume exists with different driver options: cannot modify in-place; use recreate=always to recreate"}
				}

				// Labels can't be updated on existing volumes
				if req.Labels != nil && !docker.CompareMaps(existing.Labels, req.Labels) {
					return Response{Failed: true, Msg: "volume exists with different labels: cannot modify in-place; use recreate=always to recreate"}
				}

				return Response{
					Changed:    false,
					Msg:        "volume already exists",
					VolumeID:   existing.Name,
					Name:       existing.Name,
					Driver:     existing.Driver,
					Mountpoint: existing.Mountpoint,
					CreatedAt:  existing.CreatedAt,
					Scope:      existing.Scope,
					Labels:     existing.Labels,
					Options:    existing.Options,
				}
			}
		}

		// Create
		opts := client.VolumeCreateOptions{
			Name:       req.Name,
			Driver:     req.Driver,
			DriverOpts: req.DriverOptions,
			Labels:     req.Labels,
		}

		resp, err := cli.VolumeCreate(ctx, opts)
		if err != nil {
			return Response{Failed: true, Msg: docker.WrapError("create volume", req.Name, err).Error()}
		}
		created := resp.Volume

		return Response{
			Changed:    true,
			Msg:        "volume created",
			VolumeID:   created.Name,
			Name:       created.Name,
			Driver:     created.Driver,
			Mountpoint: created.Mountpoint,
			CreatedAt:  created.CreatedAt,
			Scope:      created.Scope,
			Labels:     created.Labels,
			Options:    created.Options,
		}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}

// isVolumeInUseError checks if the error indicates the volume is in use by a container
func isVolumeInUseError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "volume is in use") ||
		strings.Contains(errStr, "volume is being used") ||
		strings.Contains(errStr, "in use by container")
}
