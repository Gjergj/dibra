package docker_image_info

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(docker.WrapError("create docker client", "", err).Error())
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	if len(req.Name) == 0 {
		summaries, err := apiClient.ImageList(ctx, client.ImageListOptions{All: false})
		if err != nil {
			return failedResponse(docker.WrapError("list images", "", err).Error())
		}
		images := make([]map[string]any, 0, len(summaries.Items))
		for _, summary := range summaries.Items {
			image, found, inspectErr := inspectImageMap(ctx, apiClient, summary.ID)
			if inspectErr != nil {
				return failedResponse(fmt.Sprintf("Error inspecting image %s - %v", summary.ID, inspectErr))
			}
			if !found {
				images = append(images, nil)
				continue
			}
			images = append(images, image)
		}
		return Response{Images: images}
	}

	images := make([]map[string]any, 0, len(req.Name))
	for _, requestedName := range req.Name {
		reference := requestedName
		if !isImageID(reference) {
			reference, err = docker.JoinImageNameTag(reference, "latest")
			if err != nil {
				return failedResponse(fmt.Sprintf("invalid image name %q: %v", requestedName, err))
			}
		}
		image, found, inspectErr := inspectImageMap(ctx, apiClient, reference)
		if inspectErr != nil {
			return failedResponse(docker.WrapError("inspect image", reference, inspectErr).Error())
		}
		if !found {
			continue
		}
		images = append(images, image)
	}
	return Response{Images: images}
}

func inspectImageMap(ctx context.Context, apiClient client.APIClient, reference string) (map[string]any, bool, error) {
	var raw bytes.Buffer
	inspection, err := apiClient.ImageInspect(ctx, reference, client.ImageInspectWithRawResponse(&raw))
	if err != nil {
		if docker.IsNotFoundError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if raw.Len() > 0 {
		image, err := docker.DecodeInspection(raw.Bytes())
		return image, true, err
	}
	image, err := docker.InspectionMap(inspection)
	return image, true, err
}

func isImageID(value string) bool {
	trimmed := strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if len(trimmed) < 12 || len(trimmed) > 64 {
		return false
	}
	for _, character := range trimmed {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Images: []map[string]any{}}
}
