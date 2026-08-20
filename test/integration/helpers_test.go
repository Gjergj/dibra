//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/gjergjiramku/dibra/internal/ssh"
)

func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}

const (
	testDockerEngineVersion  = "29.7.2"
	testDockerComposeVersion = docker.SupportedComposeVersion
	testDockerBuildxVersion  = "0.30.0"
)

var (
	integrationTestConfig = defaultIntegrationConfig()
	playbookHeader        = integrationTestConfig.playbookHeader()
)

func TestMain(m *testing.M) {
	config, err := loadIntegrationConfig(os.Getenv)
	if err != nil {
		println("Invalid integration configuration:", err.Error())
		os.Exit(1)
	}
	integrationTestConfig = config
	playbookHeader = config.playbookHeader()

	if err := waitForSSH(30 * time.Second); err != nil {
		println("SSH not available:", err.Error())
		os.Exit(1)
	}
	if config.Profile.requiresDockerBaselines() {
		if err := requireDockerEngineVersion(); err != nil {
			println("Docker Engine baseline unavailable:", err.Error())
			os.Exit(1)
		}
		if err := requireDockerComposeVersion(); err != nil {
			println("Docker Compose baseline unavailable:", err.Error())
			os.Exit(1)
		}
		if err := requireDockerBuildxVersion(); err != nil {
			println("Docker buildx baseline unavailable:", err.Error())
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func requireDockerEngineVersion() error {
	client, err := ssh.Connect(integrationSSHConfig())
	if err != nil {
		return err
	}
	defer client.Close()
	stdout, stderr, err := client.Run("docker version --format '{{.Server.Version}}'")
	if err != nil {
		return fmt.Errorf("inspect Docker Engine version: %w (%s)", err, strings.TrimSpace(stderr))
	}
	if actual := strings.TrimSpace(stdout); actual != testDockerEngineVersion {
		return fmt.Errorf("got %s, require exact %s", actual, testDockerEngineVersion)
	}
	return nil
}

func requireDockerComposeVersion() error {
	client, err := ssh.Connect(integrationSSHConfig())
	if err != nil {
		return err
	}
	defer client.Close()
	stdout, stderr, err := client.Run("docker compose version --format json")
	if err != nil {
		return fmt.Errorf("inspect Docker Compose version: %w (%s)", err, strings.TrimSpace(stderr))
	}
	var response struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil || response.Version == "" {
		return fmt.Errorf("parse Docker Compose version output %q", strings.TrimSpace(stdout))
	}
	if actual := strings.TrimPrefix(strings.TrimSpace(response.Version), "v"); actual != testDockerComposeVersion {
		return fmt.Errorf("got %s, require exact %s", actual, testDockerComposeVersion)
	}
	return nil
}

func requireDockerBuildxVersion() error {
	client, err := ssh.Connect(integrationSSHConfig())
	if err != nil {
		return err
	}
	defer client.Close()
	stdout, stderr, err := client.Run("docker buildx version")
	if err != nil {
		return fmt.Errorf("inspect Docker buildx version: %w (%s)", err, strings.TrimSpace(stderr))
	}
	fields := strings.Fields(stdout)
	if len(fields) < 2 {
		return fmt.Errorf("parse Docker buildx version output %q", strings.TrimSpace(stdout))
	}
	actual := strings.TrimPrefix(fields[1], "v")
	if actual != testDockerBuildxVersion {
		return fmt.Errorf("got %s, require exact %s", actual, testDockerBuildxVersion)
	}
	return nil
}

func waitForSSH(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		client, err := ssh.Connect(integrationSSHConfig())
		if err == nil {
			client.Close()
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	if lastErr == nil {
		return fmt.Errorf("SSH did not become ready within %s", timeout)
	}
	return fmt.Errorf("SSH did not become ready within %s: %w", timeout, lastErr)
}

func getClient(t *testing.T) *ssh.Client {
	client, err := ssh.Connect(integrationSSHConfig())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return client
}

func runPlaybook(t *testing.T, playbook string) string {
	return runPlaybookWithArgs(t, playbook)
}

func runPlaybookWithArgs(t *testing.T, playbook string, args ...string) string {
	tmpFile := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(tmpFile, []byte(playbook), 0644); err != nil {
		t.Fatalf("Failed to write playbook: %v", err)
	}

	projectRoot := getProjectRoot()
	commandArgs := []string{"run", "./cmd/controller", "-config", tmpFile, "-v", "-force-agent-upload", "-agent-build"}
	commandArgs = append(commandArgs, args...)
	cmd := exec.Command("go", commandArgs...)
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Playbook output:\n%s", string(output))
	}
	return string(output)
}

func remoteExec(t *testing.T, client *ssh.Client, cmd string) string {
	stdout, stderr, err := client.Run(cmd)
	if err != nil {
		t.Logf("Command failed: %s\nstdout: %s\nstderr: %s", cmd, stdout, stderr)
	}
	return strings.TrimSpace(stdout)
}

func remoteFileExists(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -e " + path)
	return err == nil
}

func remoteFileContent(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "cat "+path)
}

func remoteFileMode(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %a "+path)
}

func remoteDirExists(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -d " + path)
	return err == nil
}

func remoteIsSymlink(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -L " + path)
	return err == nil
}

func remoteSymlinkTarget(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "readlink "+path)
}

func remotePackageInstalled(t *testing.T, client *ssh.Client, pkg string) bool {
	_, _, err := client.Run("dpkg -s " + pkg)
	return err == nil
}

func remoteFileOwner(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %U "+path)
}

func remoteFileGroup(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %G "+path)
}

func remoteFileInode(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %i "+path)
}

func remoteFileMtime(t *testing.T, client *ssh.Client, path string) string {
	return remoteExec(t, client, "stat -c %Y "+path)
}

func remoteIsFile(t *testing.T, client *ssh.Client, path string) bool {
	_, _, err := client.Run("test -f " + path)
	return err == nil
}

func integrationSSHConfig() ssh.Config {
	return ssh.Config{
		Host:     integrationTestConfig.Host,
		Port:     integrationTestConfig.Port,
		User:     integrationTestConfig.User,
		Password: integrationTestConfig.Password,
	}
}

func testUserPlaybookHeader(become bool) string {
	becomePassword := ""
	if become {
		becomePassword = "testpass"
	}
	return integrationTestConfig.playbookHeaderFor("testuser", "testpass", become, becomePassword)
}
