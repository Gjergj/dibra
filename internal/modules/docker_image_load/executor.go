package docker_image_load

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func Execute(req Request) Response {
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	// Check archive exists
	if _, err := os.Stat(req.Path); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("archive not found: %s", req.Path)}
	}

	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	// Open archive file
	file, err := os.Open(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to open archive: %v", err)}
	}
	defer file.Close()

	// Load images from tar
	resp, err := cli.ImageLoad(ctx, file, true)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to load images: %v", err)}
	}
	defer resp.Body.Close()

	// Read output
	output, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read response: %v", err)}
	}

	outputStr := string(output)

	// Parse loaded image names from output
	var imageNames []string
	for _, line := range strings.Split(outputStr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Loaded image:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "Loaded image:"))
			if name != "" {
				imageNames = append(imageNames, name)
			}
		} else if strings.HasPrefix(line, "Loaded image ID:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "Loaded image ID:"))
			if name != "" {
				imageNames = append(imageNames, name)
			}
		}
	}

	return Response{
		Changed:    true,
		ImageNames: imageNames,
		Stdout:     outputStr,
		Msg:        "images loaded successfully",
	}
}
