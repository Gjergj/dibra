package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// LocalClient preserves the controller/agent process boundary while replacing
// SSH and SCP with local process and filesystem operations.
type LocalClient struct {
	ctx context.Context
}

func NewLocalClient(ctx context.Context) *LocalClient {
	if ctx == nil {
		ctx = context.Background()
	}
	return &LocalClient{ctx: ctx}
}

func (c *LocalClient) Close() error { return nil }

func (c *LocalClient) Reconnect() (ExecutionClient, error) {
	return nil, fmt.Errorf("local execution client does not support reconnect")
}

func (c *LocalClient) IsLocal() bool { return true }

func (c *LocalClient) FileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (c *LocalClient) ExecuteAgent(agentPath string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(c.ctx, agentPath)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, fmt.Errorf("failed to execute local agent: %w", err)
		}
		if stdout.Len() == 0 {
			return nil, fmt.Errorf("local agent failed: %w: %s", err, stderr.String())
		}
	}
	return stdout.Bytes(), nil
}

func (c *LocalClient) UploadFile(localPath, destinationPath string) error {
	return copyLocalFile(localPath, destinationPath, 0o755)
}

func (c *LocalClient) DownloadFile(sourcePath, destinationPath string) error {
	return copyLocalFile(sourcePath, destinationPath, 0o644)
}

func (c *LocalClient) Run(command string) (string, string, error) {
	return c.run(command)
}

func (c *LocalClient) RunWithSudo(command string) (string, string, error) {
	return c.run(command)
}

func (c *LocalClient) run(command string) (string, string, error) {
	cmd := exec.CommandContext(c.ctx, "/bin/sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func copyLocalFile(sourcePath, destinationPath string, defaultMode os.FileMode) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	destinationAbs, err := filepath.Abs(destinationPath)
	if err != nil {
		return fmt.Errorf("resolve destination path: %w", err)
	}
	if sourceAbs == destinationAbs {
		return nil
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer source.Close()

	mode := defaultMode
	if info, statErr := source.Stat(); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(destinationPath), ".dibra-local-copy-*")
	if err != nil {
		return fmt.Errorf("create temporary destination: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := io.Copy(temporary, source); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy file data: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set destination mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary destination: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	return nil
}
