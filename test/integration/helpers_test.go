//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func getProjectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}

const (
	testHost     = "localhost"
	testPort     = 2222
	testUser     = "root"
	testPassword = "rootpass"
)

func TestMain(m *testing.M) {
	if err := waitForSSH(30 * time.Second); err != nil {
		println("SSH not available:", err.Error())
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitForSSH(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ssh.Connect(ssh.Config{
			Host:     testHost,
			Port:     testPort,
			User:     testUser,
			Password: testPassword,
		})
		if err == nil {
			client.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return nil
}

func getClient(t *testing.T) *ssh.Client {
	client, err := ssh.Connect(ssh.Config{
		Host:     testHost,
		Port:     testPort,
		User:     testUser,
		Password: testPassword,
	})
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return client
}

func runPlaybook(t *testing.T, playbook string) string {
	tmpFile := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(tmpFile, []byte(playbook), 0644); err != nil {
		t.Fatalf("Failed to write playbook: %v", err)
	}

	projectRoot := getProjectRoot()
	cmd := exec.Command("go", "run", "./cmd/controller", "-config", tmpFile, "-v", "-force-agent-upload", "-agent-build")
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

const playbookHeader = `
hosts:
  - name: testhost
    host: localhost
    port: 2222
    user: root
    password: rootpass
    become: true

tasks:
`
