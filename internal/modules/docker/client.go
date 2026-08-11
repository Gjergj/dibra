package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// GetContainerUser returns the user configured for the container
func GetContainerUser(ctx context.Context, cli client.APIClient, containerID string) (string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	user := inspect.Container.Config.User
	if user == "" {
		return "root", nil
	}
	return user, nil
}

// GetContainerUserIDs returns the numeric UID and GID for a given user in the container
func GetContainerUserIDs(ctx context.Context, cli client.APIClient, containerID string, user string) (uid, gid int, err error) {
	if user == "" {
		user = "root"
	}

	execConfig := client.ExecCreateOptions{
		Cmd:          []string{"sh", "-c", "id -u && id -g"},
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
	}

	execResp, err := cli.ExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return 0, 0, err
	}

	hijack, err := cli.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return 0, 0, err
	}
	defer hijack.Close()

	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, hijack.Reader)
	if err != nil && err != io.EOF {
		return 0, 0, err
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("failed to get UID/GID, output: %q", stdout.String())
	}

	uid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse UID: %v", err)
	}

	gid, err = strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		return uid, 0, fmt.Errorf("failed to parse GID: %v", err)
	}

	return uid, gid, nil
}

// Ping checks connectivity
func Ping(ctx context.Context, cli client.APIClient) error {
	_, err := cli.Ping(ctx, client.PingOptions{})
	return err
}
