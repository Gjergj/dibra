package docker_container_copy_into

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	cli, err := docker.GetClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err)}
	}
	defer cli.Close()

	ctx, cancel := docker.GetContext(req.CommonArgs)
	defer cancel()

	if req.Container == "" {
		return Response{Failed: true, Msg: "container is required"}
	}
	if req.ContainerPath == "" {
		return Response{Failed: true, Msg: "container_path is required"}
	}
	if req.Path == "" && req.Content == "" {
		return Response{Failed: true, Msg: "either path or content must be provided"}
	}
	if req.Path != "" && req.Content != "" {
		return Response{Failed: true, Msg: "path and content are mutually exclusive"}
	}

	// Determine file content
	var fileContent []byte
	var fileMode os.FileMode = 0644

	if req.Content != "" {
		if req.ContentIsB64 {
			decoded, err := base64.StdEncoding.DecodeString(req.Content)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to decode base64 content: %v", err)}
			}
			fileContent = decoded
		} else {
			fileContent = []byte(req.Content)
		}
	} else {
		// Read from local path
		localPath := req.Path
		if req.LocalFollow {
			// Resolve symlinks
			resolved, err := filepath.EvalSymlinks(localPath)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to resolve symlink: %v", err)}
			}
			localPath = resolved
		}

		info, err := os.Stat(localPath)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to stat local file: %v", err)}
		}
		if !info.Mode().IsRegular() {
			return Response{Failed: true, Msg: "local path must be a regular file"}
		}

		fileContent, err = os.ReadFile(localPath)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to read local file: %v", err)}
		}
		fileMode = info.Mode().Perm()
	}

	// Parse mode if provided
	if req.Mode != "" {
		parsed, err := strconv.ParseInt(req.Mode, 8, 32)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to parse mode: %v", err)}
		}
		fileMode = os.FileMode(parsed)
	}

	// Determine owner/group IDs
	ownerID := 0
	groupID := 0
	if req.OwnerID != nil {
		ownerID = *req.OwnerID
	}
	if req.GroupID != nil {
		groupID = *req.GroupID
	}

	// If not explicitly provided, try to detect container default user IDs
	if req.OwnerID == nil || req.GroupID == nil {
		user, err := docker.GetContainerUser(ctx, cli, req.Container)
		if err == nil {
			uid, gid, err := docker.GetContainerUserIDs(ctx, cli, req.Container, user)
			if err == nil {
				if req.OwnerID == nil {
					ownerID = uid
				}
				if req.GroupID == nil {
					groupID = gid
				}
			}
		}
	}

	// Create tar archive with the file
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Get just the filename from container path
	fileName := filepath.Base(req.ContainerPath)

	hdr := &tar.Header{
		Name:    fileName,
		Mode:    int64(fileMode),
		Size:    int64(len(fileContent)),
		Uid:     ownerID,
		Gid:     groupID,
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write tar header: %v", err)}
	}

	if _, err := tw.Write(fileContent); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write tar content: %v", err)}
	}

	if err := tw.Close(); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to close tar writer: %v", err)}
	}

	// Copy to container
	destDir := filepath.Dir(req.ContainerPath)
	copyOptions := client.CopyToContainerOptions{
		DestinationPath:           destDir,
		Content:                   &tarBuf,
		AllowOverwriteDirWithFile: true,
	}

	_, err = cli.CopyToContainer(ctx, req.Container, copyOptions)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to copy to container: %v", err)}
	}

	return Response{
		Changed:       true,
		ContainerPath: req.ContainerPath,
		Msg:           "file copied to container",
	}
}
