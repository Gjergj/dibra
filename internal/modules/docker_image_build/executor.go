package docker_image_build

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// Regex patterns for parsing build output
var (
	imageIDPattern = regexp.MustCompile(`(?i)writing image sha256:([a-f0-9]{64})`)
	digestPattern  = regexp.MustCompile(`(?i)digest:\s*(sha256:[a-f0-9]{64})`)
	errorPattern   = regexp.MustCompile(`(?i)^error\b|^#\d+ ERROR`)
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

	// Check if image already exists and get its ID (for idempotency)
	var existingImageID string
	if rebuild == "never" {
		cli, err := docker.GetClient(req.CommonArgs)
		if err == nil {
			defer cli.Close()
			inspect, _, err := cli.ImageInspectWithRaw(context.Background(), fullImageName)
			if err == nil {
				existingImageID = inspect.ID
				// Image exists, no change needed (unless we need to compare build context)
				return Response{
					Changed: false,
					Msg:     "image already exists",
					Image:   map[string]string{"name": req.Name, "tag": tag},
					ImageID: inspect.ID,
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

	// Parse build output for errors, image ID, and digest
	logs := parseLogLines(outputStr)
	imageID := extractImageID(outputStr)
	digest := extractDigest(outputStr)
	buildErrors := extractErrors(outputStr)

	if err != nil {
		errMsg := fmt.Sprintf("build failed: %v", err)
		if len(buildErrors) > 0 {
			errMsg = fmt.Sprintf("build failed: %s", strings.Join(buildErrors, "; "))
		}
		return Response{
			Failed: true,
			Msg:    errMsg,
			Stdout: outputStr,
			Logs:   logs,
		}
	}

	// Check idempotency by comparing image IDs
	changed := true
	if existingImageID != "" && imageID != "" {
		if existingImageID == imageID {
			changed = false
		}
	}

	return Response{
		Changed: changed,
		Msg:     "image built successfully",
		Image:   map[string]string{"name": req.Name, "tag": tag},
		ImageID: imageID,
		Digest:  digest,
		Stdout:  outputStr,
		Logs:    logs,
	}
}

// parseLogLines extracts log lines from build output
func parseLogLines(output string) []string {
	lines := strings.Split(output, "\n")
	var logs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			logs = append(logs, line)
		}
	}
	return logs
}

// extractImageID extracts the built image ID from build output
func extractImageID(output string) string {
	matches := imageIDPattern.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return "sha256:" + matches[1]
	}
	return ""
}

// extractDigest extracts the pushed digest from build output
func extractDigest(output string) string {
	matches := digestPattern.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// extractErrors extracts error messages from build output
func extractErrors(output string) []string {
	lines := strings.Split(output, "\n")
	var errors []string
	for _, line := range lines {
		if errorPattern.MatchString(line) {
			errors = append(errors, strings.TrimSpace(line))
		}
	}
	return errors
}
