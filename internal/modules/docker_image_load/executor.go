package docker_image_load

import (
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
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	// Check archive exists
	if _, err := dependencies.FileSystem.Stat(req.Path); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("archive not found: %s", req.Path)}
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	// Open archive file
	file, err := dependencies.FileSystem.Open(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to open archive: %v", err)}
	}
	defer file.Close()

	// Load images from tar
	resp, err := cli.ImageLoad(ctx, file, client.ImageLoadWithQuiet(true))
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to load images: %v", err)}
	}
	defer resp.Close()

	result := docker.ParseLoadStream(resp)
	if result.Error != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to load images: %v", result.Error)}
	}
	if len(result.Images) == 0 {
		return Response{Failed: true, Msg: "detected no loaded images; archive may be corrupt", Stdout: strings.Join(result.Logs, "\n")}
	}
	outputStr := strings.Join(result.Logs, "\n")

	return Response{
		Changed:    true,
		ImageNames: result.Images,
		Stdout:     outputStr,
		Msg:        "images loaded successfully",
	}
}
