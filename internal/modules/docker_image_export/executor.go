package docker_image_export

import (
	"fmt"
	"io"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	if len(req.Names) == 0 {
		return Response{Failed: true, Msg: "names is required"}
	}
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	// Default tag
	tag := req.Tag
	if tag == "" {
		tag = "latest"
	}

	// Prepare list of images to export and verify they exist
	var images []map[string]interface{}
	var exportNames []string
	for _, name := range req.Names {
		fullImageName := name
		if !strings.Contains(name, ":") && !strings.Contains(name, "@") {
			fullImageName = fmt.Sprintf("%s:%s", name, tag)
		}

		inspect, err := cli.ImageInspect(ctx, fullImageName)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("image not found: %s", fullImageName)}
		}

		imageInfo := map[string]interface{}{
			"Id":       inspect.ID,
			"RepoTags": inspect.RepoTags,
		}
		images = append(images, imageInfo)
		exportNames = append(exportNames, fullImageName)
	}

	// Idempotency check: if file exists and Force is false, skip
	if !req.Force {
		if _, err := dependencies.FileSystem.Stat(req.Path); err == nil {
			// In a more complete implementation, we'd check if the archived image matches
			// but for now, simple file existence is a start for "not changed"
			return Response{
				Changed: false,
				Msg:     "archive already exists",
				Images:  images,
			}
		}
	}

	// Export images
	readCloser, err := cli.ImageSave(ctx, exportNames)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to export images: %v", err)}
	}
	defer readCloser.Close()

	// Create/Overwrite archive file
	file, err := dependencies.FileSystem.Create(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create archive file: %v", err)}
	}
	defer file.Close()

	_, err = io.Copy(file, readCloser)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write archive file: %v", err)}
	}

	return Response{
		Changed: true,
		Images:  images,
		Msg:     "images exported successfully",
	}
}
