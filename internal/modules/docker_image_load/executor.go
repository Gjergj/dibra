package docker_image_load

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	result := Response{ImageNames: []string{}, Images: []map[string]any{}}
	if req.Path == "" {
		result.Failed = true
		result.Msg = "path is required"
		return result
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("failed to create docker client: %v", err)
		return result
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	archive, err := dependencies.FileSystem.Open(req.Path)
	if err != nil {
		result.Failed = true
		if errors.Is(err, fs.ErrNotExist) {
			result.Msg = fmt.Sprintf("Error opening archive %s - %v", req.Path, err)
		} else {
			result.Msg = fmt.Sprintf("Error loading archive %s - %v", req.Path, err)
		}
		return result
	}
	defer archive.Close()

	response, err := apiClient.ImageLoad(ctx, archive)
	if err != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error loading archive %s - %v", req.Path, err)
		return result
	}
	defer response.Close()

	stream := docker.ParseLoadStream(response)
	result.Stdout = strings.Join(stream.Logs, "\n")
	if stream.Error != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error loading archive %s - %v", req.Path, stream.Error)
		return result
	}
	if len(stream.Images) == 0 {
		result.Failed = true
		result.Msg = "Detected no loaded images. Archive potentially corrupt?"
		return result
	}

	result.ImageNames = append(result.ImageNames, stream.Images...)
	for _, imageName := range stream.Images {
		if !isImageID(imageName) && !strings.Contains(imageName, ":") {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Image name %q is neither ID nor has a tag", imageName))
			continue
		}
		inspection, inspectErr := apiClient.ImageInspect(ctx, imageName)
		if inspectErr != nil {
			if docker.IsNotFoundError(inspectErr) {
				result.Images = append(result.Images, nil)
				continue
			}
			result.Failed = true
			result.Msg = fmt.Sprintf("Error inspecting loaded image %s - %v", imageName, inspectErr)
			return result
		}
		image, conversionErr := inspectionMap(inspection)
		if conversionErr != nil {
			result.Failed = true
			result.Msg = conversionErr.Error()
			return result
		}
		result.Images = append(result.Images, image)
	}
	result.Changed = true
	return result
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
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	hash := strings.TrimPrefix(value, "sha256:")
	if len(hash) != 64 {
		return false
	}
	for _, character := range hash {
		if character < '0' || character > '9' && character < 'A' || character > 'F' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
