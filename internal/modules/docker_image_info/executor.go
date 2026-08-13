package docker_image_info

import (
	"encoding/json"
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
			inspection, err := apiClient.ImageInspect(ctx, summary.ID)
			if err != nil {
				if docker.IsNotFoundError(err) {
					images = append(images, nil)
					continue
				}
				return failedResponse(fmt.Sprintf("Error inspecting image %s - %v", summary.ID, err))
			}
			image, err := inspectionMap(inspection)
			if err != nil {
				return failedResponse(err.Error())
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
		inspection, inspectErr := apiClient.ImageInspect(ctx, reference)
		if inspectErr != nil {
			if docker.IsNotFoundError(inspectErr) {
				continue
			}
			return failedResponse(docker.WrapError("inspect image", reference, inspectErr).Error())
		}
		image, err := inspectionMap(inspection)
		if err != nil {
			return failedResponse(err.Error())
		}
		images = append(images, image)
	}
	return Response{Images: images}
}

func inspectionMap(value client.ImageInspectResult) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode image inspection: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode image inspection: %w", err)
	}
	return result, nil
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
