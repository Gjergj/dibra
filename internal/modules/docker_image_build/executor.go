package docker_image_build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	// Validate inputs
	if req.Name == "" {
		return Response{Failed: true, Msg: "name is required"}
	}
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	// Check build context exists
	if info, err := os.Stat(req.Path); err != nil || !info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("build path does not exist or is not a directory: %s", req.Path)}
	}

	// Apply defaults
	tag := req.Tag
	if tag == "" {
		tag = "latest"
	}

	rebuild := req.Rebuild
	if rebuild == "" {
		rebuild = "never"
	}

	fullImageName := fmt.Sprintf("%s:%s", req.Name, tag)

	// Check if image already exists (for idempotency)
	if rebuild == "never" {
		cli, err := docker.GetClient(req.CommonArgs)
		if err == nil {
			defer cli.Close()
			_, _, err := cli.ImageInspectWithRaw(context.Background(), fullImageName)
			if err == nil {
				// Image exists, no change needed
				return Response{
					Changed: false,
					Msg:     "image already exists",
					Image:   map[string]string{"name": req.Name, "tag": tag},
				}
			}
		}
	}

	// Build args for docker buildx build
	args := []string{"buildx", "build", "--progress", "plain"}

	// Tag
	args = append(args, "--tag", fullImageName)

	// Dockerfile
	if req.Dockerfile != "" {
		args = append(args, "--file", filepath.Join(req.Path, req.Dockerfile))
	}

	// Cache from
	for _, cache := range req.CacheFrom {
		args = append(args, "--cache-from", cache)
	}

	// Pull
	if req.Pull {
		args = append(args, "--pull")
	}

	// Network
	if req.Network != "" {
		args = append(args, "--network", req.Network)
	}

	// No cache
	if req.NoCache {
		args = append(args, "--no-cache")
	}

	// Etc hosts
	for host, ip := range req.EtcHosts {
		args = append(args, "--add-host", fmt.Sprintf("%s:%s", host, ip))
	}

	// Build args
	for key, value := range req.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Target
	if req.Target != "" {
		args = append(args, "--target", req.Target)
	}

	// Platform
	for _, platform := range req.Platform {
		args = append(args, "--platform", platform)
	}

	// Shm size
	if req.ShmSize != "" {
		args = append(args, "--shm-size", req.ShmSize)
	}

	// Labels
	for key, value := range req.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", key, value))
	}

	// Push
	if req.Push {
		args = append(args, "--push")
	}

	// Load into local docker (for buildx)
	if !req.Push {
		args = append(args, "--load")
	}

	// Build context path
	args = append(args, "--", req.Path)

	// Set up environment
	env := os.Environ()
	if req.DockerHost != "" {
		env = append(env, fmt.Sprintf("DOCKER_HOST=%s", req.DockerHost))
	}
	if req.TLS {
		env = append(env, "DOCKER_TLS_VERIFY=1")
		if req.CAPath != "" {
			env = append(env, fmt.Sprintf("DOCKER_CERT_PATH=%s", req.CAPath))
		}
	}

	// Execute build
	cmd := exec.Command("docker", args...)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("build failed: %v", err),
			Stdout: outputStr,
		}
	}

	// Determine if anything actually changed
	changed := true
	lowerOut := strings.ToLower(outputStr)
	if strings.Contains(lowerOut, "exporting to image") ||
		strings.Contains(lowerOut, "built") ||
		strings.Contains(lowerOut, "running") {
		changed = true
	}

	return Response{
		Changed: changed,
		Msg:     "image built successfully",
		Image:   map[string]string{"name": req.Name, "tag": tag},
		Stdout:  outputStr,
	}
}
