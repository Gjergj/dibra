package docker_image

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("create docker client", "", err).Error()}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// Normalize Name and Tag
	ref := normalizeImageRef(req.Name, req.Tag)

	state := req.State
	if state == "" {
		state = "present"
	}

	switch state {
	case "absent":
		return handleAbsent(ctx, cli, ref, req)
	case "present":
		return handlePresent(ctx, cli, ref, req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
	}
}

func normalizeImageRef(name, tag string) string {
	if tag != "" {
		return fmt.Sprintf("%s:%s", name, tag)
	}
	return name
}

// handleAbsent removes an image
func handleAbsent(ctx context.Context, cli *client.Client, ref string, req Request) Response {
	// Check if exists
	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if docker.IsNotFoundError(err) {
		return Response{Changed: false, Msg: "image already absent"}
	}
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("inspect image", ref, err).Error()}
	}

	// Determine force flag (3.5: prefer ForceRemove, fall back to ForceSource for backward compat)
	force := req.ForceRemove || req.ForceSource

	// Remove
	opts := types.ImageRemoveOptions{
		Force:         force,
		PruneChildren: true,
	}
	_, err = cli.ImageRemove(ctx, ref, opts)
	if err != nil {
		return Response{Failed: true, Msg: docker.WrapError("remove image", ref, err).Error()}
	}
	return Response{Changed: true, Msg: "image removed"}
}

// handlePresent ensures an image is present
func handlePresent(ctx context.Context, cli *client.Client, ref string, req Request) Response {
	source := req.Source
	if source == "" {
		source = "pull"
	}

	switch source {
	case "pull":
		return handlePull(ctx, cli, ref, req)
	case "local":
		return handleLocal(ctx, cli, ref, req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unsupported source: %s (supported: pull, local)", source)}
	}
}

// handlePull pulls an image from registry (3.1, 3.2)
func handlePull(ctx context.Context, cli *client.Client, ref string, req Request) Response {
	// Determine pull policy (3.2, 3.5: handle backward compat)
	pullPolicy := req.Pull
	if pullPolicy == "" {
		pullPolicy = PullMissing
	}
	// ForceSource/ForcePull override to "always" for backward compat
	if req.ForcePull || req.ForceSource {
		pullPolicy = PullAlways
	}

	// Get existing image ID before pull (3.2.3)
	var existingID string
	if inspect, _, err := cli.ImageInspectWithRaw(ctx, ref); err == nil {
		existingID = inspect.ID
	}

	// Decide whether to pull based on policy
	switch pullPolicy {
	case PullNever:
		if existingID == "" {
			return Response{Failed: true, Msg: fmt.Sprintf("image %s not found locally and pull is 'never'", ref)}
		}
		return Response{Changed: false, Msg: "image already present (pull: never)", ImageID: existingID}

	case PullMissing:
		if existingID != "" {
			// Image already exists, no need to pull
			return Response{Changed: false, Msg: "image already present", ImageID: existingID}
		}
		// Fall through to pull

	case PullAlways:
		// Always pull, even if exists
	}

	// Pull the image (3.1, 3.2)
	changed, newID, digest, err := pullImage(ctx, cli, ref, existingID, req.RegistryUsername, req.RegistryPassword)
	if err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	if !changed {
		return Response{Changed: false, Msg: "image already up to date", ImageID: newID, Digest: digest}
	}

	return Response{Changed: true, Msg: "image pulled", ImageID: newID, Digest: digest}
}

// pullImage performs the actual image pull (3.1, 3.2)
func pullImage(ctx context.Context, cli *client.Client, image, existingID, username, password string) (changed bool, imageID, digest string, err error) {
	// Encode registry auth (3.1.3)
	registryAuth := docker.EncodeRegistryAuth(username, password)

	pullOpts := types.ImagePullOptions{}
	if registryAuth != "" {
		pullOpts.RegistryAuth = registryAuth
	}

	reader, err := cli.ImagePull(ctx, image, pullOpts)
	if err != nil {
		return false, "", "", docker.WrapError("pull image", image, err)
	}
	defer reader.Close()

	// Parse stream for errors and digest (3.2.1, 3.2.2)
	result := docker.ParsePullPushStream(reader)
	if result.Error != nil {
		return false, "", "", docker.WrapError("pull image", image, result.Error)
	}

	// Get the new image ID
	inspect, _, err := cli.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return false, "", "", docker.WrapError("inspect image after pull", image, err)
	}

	// Compare image IDs to detect actual changes (3.2.3, 3.2.4)
	if existingID != "" && inspect.ID == existingID {
		return false, inspect.ID, result.Digest, nil
	}

	return true, inspect.ID, result.Digest, nil
}

// handleLocal handles source=local (tag and optionally push)
func handleLocal(ctx context.Context, cli *client.Client, ref string, req Request) Response {
	// Check source image exists
	inspect, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		if docker.IsNotFoundError(err) {
			return Response{Failed: true, Msg: fmt.Sprintf("image %s not found locally", ref)}
		}
		return Response{Failed: true, Msg: docker.WrapError("inspect image", ref, err).Error()}
	}

	// If no repository specified, just return the image info
	if req.Repository == "" {
		return Response{Changed: false, ImageID: inspect.ID}
	}

	// Tag the image (3.3)
	targetRef := normalizeImageRef(req.Repository, req.Tag)
	changed, err := tagImage(ctx, cli, ref, targetRef, inspect.ID, req.ForceTag)
	if err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}

	// Push if requested (3.4)
	if req.Push {
		digest, err := pushImage(ctx, cli, targetRef, req.RegistryUsername, req.RegistryPassword)
		if err != nil {
			return Response{Failed: true, Msg: err.Error()}
		}
		msg := "image pushed"
		if changed {
			msg = "image tagged and pushed"
		}
		return Response{Changed: true, Msg: msg, ImageID: inspect.ID, Digest: digest}
	}

	if changed {
		return Response{Changed: true, Msg: "image tagged", ImageID: inspect.ID}
	}
	return Response{Changed: false, Msg: "image already tagged correctly", ImageID: inspect.ID}
}

// tagImage tags an image with idempotency (3.3)
func tagImage(ctx context.Context, cli *client.Client, sourceRef, targetRef, sourceID string, force bool) (changed bool, err error) {
	// Check if target already exists with same image ID (3.3.1, 3.3.2)
	if !force {
		if targetInspect, _, err := cli.ImageInspectWithRaw(ctx, targetRef); err == nil {
			if targetInspect.ID == sourceID {
				// Already tagged correctly (3.3.4)
				return false, nil
			}
		}
	}

	// Tag the image (3.3.3)
	if err := cli.ImageTag(ctx, sourceRef, targetRef); err != nil {
		return false, docker.WrapError("tag image", targetRef, err)
	}

	return true, nil
}

// pushImage pushes an image to registry (3.4)
func pushImage(ctx context.Context, cli *client.Client, ref, username, password string) (digest string, err error) {
	// Encode registry auth (3.4.3)
	registryAuth := docker.EncodeRegistryAuth(username, password)

	pushOpts := types.ImagePushOptions{}
	if registryAuth != "" {
		pushOpts.RegistryAuth = registryAuth
	}

	reader, err := cli.ImagePush(ctx, ref, pushOpts)
	if err != nil {
		return "", docker.WrapError("push image", ref, err)
	}
	defer reader.Close()

	// Parse stream for errors and digest (3.4.1, 3.4.2)
	result := docker.ParsePullPushStream(reader)
	if result.Error != nil {
		return "", docker.WrapError("push image", ref, result.Error)
	}

	return result.Digest, nil
}
