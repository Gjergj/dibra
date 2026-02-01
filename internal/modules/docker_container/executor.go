package docker_container

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx := context.Background()

	// 1. Check if container exists
	existing, err := cli.ContainerInspect(ctx, req.Name)
	exists := err == nil
	if err != nil && !client.IsErrNotFound(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to inspect container: %v", err)}
	}

	// 2. Resolve state
	state := req.State
	if state == "" {
		state = "started" // Default
	}

	switch state {
	case "absent":
		if !exists {
			return Response{Changed: false, Msg: "container already absent"}
		}
		return removeContainer(ctx, cli, req.Name, req.ForceKill, req.KeepVolumes)

	case "stopped":
		if !exists {
			return Response{Changed: false, Msg: "container not found"} // Or should we ignore? Ansible usually is idempotent.
		}
		if !existing.State.Running {
			return Response{Changed: false, Msg: "container already stopped", Container: convertContainer(existing)}
		}
		return stopContainer(ctx, cli, req.Name, req.ForceKill)

	case "started", "present":
		// Handle creation/start
		if !exists {
			return createAndStart(ctx, cli, req)
		}

		// Check idempotency / updates
		// For Phase 1: Simple check (running state and image)
		// TODO: Deep compare config

		needsRecreate := false

		// Check image
		if req.Image != "" && existing.Config.Image != req.Image && !strings.HasSuffix(existing.Config.Image, req.Image) {
			// Compare IDs if possible, but for now simple string check
			// Ideally we resolve req.Image to ID and compare existing.Image (ID)
			needsRecreate = true
		}

		if needsRecreate {
			// Recreate
			resp := removeContainer(ctx, cli, req.Name, req.ForceKill, req.KeepVolumes)
			if resp.Failed {
				return resp
			}
			return createAndStart(ctx, cli, req)
		}

		if state == "started" && !existing.State.Running {
			return startContainer(ctx, cli, req.Name)
		}

		return Response{Changed: false, Container: convertContainer(existing)}

	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", state)}
	}
}

func createAndStart(ctx context.Context, cli *client.Client, req Request) Response {
	// Pull image if needed
	if req.Pull && req.Image != "" {
		// naive pull
		reader, err := cli.ImagePull(ctx, req.Image, types.ImagePullOptions{})
		if err == nil {
			defer reader.Close()
			io.Copy(io.Discard, reader) // consume output
		}
		// If error, we might still proceed if image exists locally.
		// Ideally we check error type.
	}

	// Config
	config := &container.Config{
		Image:      req.Image,
		Env:        convertEnv(req.Env),
		Hostname:   req.Hostname,
		Domainname: req.Domainname,
		User:       req.User,
		WorkingDir: req.WorkingDir,
		Labels:     req.Labels,
	}

	if req.Command != nil {
		config.Cmd = parseCommand(req.Command)
	}

	// Host Config
	hostConfig := &container.HostConfig{
		PublishAllPorts: false,
		AutoRemove:      req.AutoRemove,
		Privileged:      req.Privileged,
		NetworkMode:     container.NetworkMode(req.NetworkMode),
		Binds:           req.Volumes,
		PortBindings:    make(nat.PortMap),
	}

	// Handle Ports
	for _, p := range req.Ports {
		// format: ip:hostPort:containerPort or hostPort:containerPort
		// Very basic parsing for Phase 1
		parts := strings.Split(p, ":")
		if len(parts) >= 2 {
			containerPort := parts[len(parts)-1]
			// Assume tcp
			port := nat.Port(containerPort + "/tcp")
			hostConfig.PortBindings[port] = []nat.PortBinding{
				{HostPort: parts[len(parts)-2]},
			}
		}
	}

	// Restart Policy
	if req.RestartPolicy != "" {
		parts := strings.Split(req.RestartPolicy, ":")
		name := parts[0]
		maxRetry := 0
		if len(parts) > 1 {
			// parse int
		}
		hostConfig.RestartPolicy = container.RestartPolicy{Name: name, MaximumRetryCount: maxRetry}
	}

	// Create
	created, err := cli.ContainerCreate(ctx, config, hostConfig, nil, nil, req.Name)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create container: %v", err)}
	}

	// Connect to extra networks
	for _, n := range req.Networks {
		config := &network.EndpointSettings{
			Aliases:   n.Aliases,
			Links:     n.Links,
			IPAddress: n.IPv4Address,
		}
		if err := cli.NetworkConnect(ctx, n.Name, created.ID, config); err != nil {
			// Cleanup?
		}
	}

	if req.State == "present" {
		return Response{Changed: true, Msg: "container created", Container: map[string]interface{}{"Id": created.ID}}
	}

	// Start
	if err := cli.ContainerStart(ctx, created.ID, types.ContainerStartOptions{}); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to start container: %v", err)}
	}

	// Inspect again to return state
	inspect, _ := cli.ContainerInspect(ctx, created.ID)
	return Response{Changed: true, Msg: "container started", Container: convertContainer(inspect)}
}

func removeContainer(ctx context.Context, cli *client.Client, name string, force bool, keepVolumes bool) Response {
	opts := types.ContainerRemoveOptions{
		Force:         force,
		RemoveVolumes: !keepVolumes,
	}
	if err := cli.ContainerRemove(ctx, name, opts); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to remove container: %v", err)}
	}
	return Response{Changed: true, Msg: "container removed"}
}

func stopContainer(ctx context.Context, cli *client.Client, name string, force bool) Response {
	if force {
		if err := cli.ContainerKill(ctx, name, "SIGKILL"); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to kill container: %v", err)}
		}
	} else {
		timeout := 10 // default
		if err := cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout}); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to stop container: %v", err)}
		}
	}
	return Response{Changed: true, Msg: "container stopped"}
}

func startContainer(ctx context.Context, cli *client.Client, name string) Response {
	if err := cli.ContainerStart(ctx, name, types.ContainerStartOptions{}); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to start container: %v", err)}
	}
	inspect, _ := cli.ContainerInspect(ctx, name)
	return Response{Changed: true, Container: convertContainer(inspect)}
}

func parseCommand(cmd interface{}) []string {
	// TODO: Handle string splitting vs list
	if s, ok := cmd.(string); ok {
		return strings.Fields(s)
	}
	if list, ok := cmd.([]interface{}); ok {
		res := make([]string, len(list))
		for i, v := range list {
			res[i] = fmt.Sprint(v)
		}
		return res
	}
	return nil
}

func convertEnv(env map[string]string) []string {
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res
}

func convertContainer(c types.ContainerJSON) map[string]interface{} {
	// In a real implementation this would map to a known struct or generic map
	// For now, return what we can
	return map[string]interface{}{
		"Id":              c.ID,
		"Name":            c.Name,
		"State":           c.State,
		"NetworkSettings": c.NetworkSettings,
		"Config":          c.Config,
	}
}
