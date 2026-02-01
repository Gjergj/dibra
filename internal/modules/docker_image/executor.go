package docker_image

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx := context.Background()

	// Normalize Name and Tag
	ref := req.Name
	if req.Tag != "" {
		ref = fmt.Sprintf("%s:%s", req.Name, req.Tag)
	}

	state := req.State
	if state == "" {
		state = "present"
	}

	// === ABSENT ===
	if state == "absent" {
		// Check if exists
		_, _, err := cli.ImageInspectWithRaw(ctx, ref)
		if client.IsErrNotFound(err) {
			return Response{Changed: false, Msg: "image already absent"}
		}
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect image: %v", err)}
		}

		// Remove
		opts := types.ImageRemoveOptions{
			Force:         req.ForceSource, // Reuse force_source as force remove? Ansible uses force_absent usually.
			PruneChildren: true,
		}
		_, err = cli.ImageRemove(ctx, ref, opts)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove image: %v", err)}
		}
		return Response{Changed: true, Msg: "image removed"}
	}

	// === PRESENT ===
	if state == "present" {
		source := req.Source
		if source == "" {
			source = "pull"
		}

		switch source {
		case "pull":
			// Check if exists
			_, _, err := cli.ImageInspectWithRaw(ctx, ref)
			exists := err == nil

			if exists && !req.ForceSource {
				// Already present, no forced pull
				return Response{Changed: false, Msg: "image already present"}
			}

			// Pull
			reader, err := cli.ImagePull(ctx, ref, types.ImagePullOptions{})
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to pull image: %v", err)}
			}
			defer reader.Close()
			// Consume output to wait for pull to finish
			io.Copy(io.Discard, reader)

			return Response{Changed: true, Msg: "image pulled", ImageID: ref}

		case "local":
			// Just check existence
			inspect, _, err := cli.ImageInspectWithRaw(ctx, ref)
			if err != nil {
				if client.IsErrNotFound(err) {
					return Response{Failed: true, Msg: fmt.Sprintf("image %s not found locally", ref)}
				}
				return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect image: %v", err)}
			}

			// Tagging if Repository provided
			if req.Repository != "" {
				targetRef := req.Repository
				if req.Tag != "" {
					targetRef = fmt.Sprintf("%s:%s", req.Repository, req.Tag)
				}

				// Check if target exists and points to same ID?
				// Simple version: just tag
				err := cli.ImageTag(ctx, ref, targetRef)
				if err != nil {
					return Response{Failed: true, Msg: fmt.Sprintf("failed to tag image: %v", err)}
				}

				if req.Push {
					// Push
					reader, err := cli.ImagePush(ctx, targetRef, types.ImagePushOptions{})
					if err != nil {
						return Response{Failed: true, Msg: fmt.Sprintf("failed to push image: %v", err)}
					}
					defer reader.Close()
					io.Copy(io.Discard, reader)
					return Response{Changed: true, Msg: "image tagged and pushed", ImageID: inspect.ID}
				}
				return Response{Changed: true, Msg: "image tagged", ImageID: inspect.ID}
			}

			return Response{Changed: false, ImageID: inspect.ID}

		default:
			return Response{Failed: true, Msg: fmt.Sprintf("unsupported source: %s (only pull, local supported)", source)}
		}
	}

	return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
}
