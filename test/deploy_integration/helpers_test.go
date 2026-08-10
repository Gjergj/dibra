//go:build integration

package deploy_integration

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

const (
	testHost     = "127.0.0.1"
	testPort     = 2222
	testUser     = "root"
	testPassword = "rootpass"
	remoteRoot   = "/tmp/dibra-deploy-it"
	remoteQueue  = remoteRoot + "/queue"
	remoteResult = remoteRoot + "/results"
	requestLog   = remoteRoot + "/requests.log"
)

var repositoryRoot string

func TestMain(testMain *testing.M) {
	repositoryRoot = findRepositoryRoot()
	if err := waitForSSH(45 * time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "dibra-deploy integration setup:", err)
		os.Exit(1)
	}
	if err := installHarness(); err != nil {
		fmt.Fprintln(os.Stderr, "dibra-deploy integration setup:", err)
		_ = uninstallHarness()
		os.Exit(1)
	}
	code := testMain.Run()
	if err := uninstallHarness(); err != nil {
		fmt.Fprintln(os.Stderr, "dibra-deploy integration cleanup:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func findRepositoryRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func waitForSSH(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		client, err := newClient()
		if err == nil {
			_ = client.Close()
			return nil
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("SSH did not become ready: %w", lastErr)
}

func newClient() (*ssh.Client, error) {
	return ssh.Connect(ssh.Config{
		Host: testHost, Port: testPort, User: testUser, Password: testPassword,
	})
}

func installHarness() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	_, _ = runRemote(client, "systemctl stop dibra-deploy.service dibra-task-server-it.service >/dev/null 2>&1 || true; pkill -f '^/usr/local/bin/dibra-task-server-it' >/dev/null 2>&1 || true")
	if _, err := runRemote(client, "mkdir -p /usr/local/bin /etc/systemd/system/dibra-deploy.service.d "+remoteQueue); err != nil {
		return err
	}

	architecture, err := remoteArchitecture(client)
	if err != nil {
		return err
	}
	buildDir, err := os.MkdirTemp("", "dibra-deploy-integration-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(buildDir)

	artifacts := []struct {
		packagePath string
		localName   string
		remotePath  string
	}{
		{packagePath: "./cmd/dibra-deploy", localName: "dibra-deploy", remotePath: "/usr/local/bin/dibra-deploy"},
		{packagePath: "./cmd/agent", localName: "dibra-agent", remotePath: "/usr/local/bin/dibra-agent"},
		{packagePath: "./test/deploy_integration/taskserver", localName: "dibra-task-server-it", remotePath: "/usr/local/bin/dibra-task-server-it"},
	}
	for _, artifact := range artifacts {
		localPath := filepath.Join(buildDir, artifact.localName)
		if err := buildLinuxBinary(artifact.packagePath, localPath, architecture); err != nil {
			return err
		}
		if err := client.UploadFile(localPath, artifact.remotePath); err != nil {
			return fmt.Errorf("upload %s: %w", artifact.localName, err)
		}
	}

	generatedFiles := map[string]string{
		"integration.conf":             "[Service]\nExecStart=\nExecStart=/usr/local/bin/dibra-deploy --agent-path /usr/local/bin/dibra-agent --verbose\n",
		"dibra-task-server-it.service": "[Unit]\nDescription=Dibra deploy integration task server\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=/usr/local/bin/dibra-task-server-it --queue-dir " + remoteQueue + " --log-file " + requestLog + "\nRestart=on-failure\n\n[Install]\nWantedBy=multi-user.target\n",
		"dibra-fake-reboot":            "#!/bin/sh\nprintf fake-reboot > " + remoteRoot + "/fake-reboot.txt\n",
	}
	remoteGenerated := map[string]string{
		"integration.conf":             "/etc/systemd/system/dibra-deploy.service.d/integration.conf",
		"dibra-task-server-it.service": "/etc/systemd/system/dibra-task-server-it.service",
		"dibra-fake-reboot":            "/usr/local/bin/dibra-fake-reboot",
	}
	for name, contents := range generatedFiles {
		localPath := filepath.Join(buildDir, name)
		if err := os.WriteFile(localPath, []byte(contents), 0o600); err != nil {
			return err
		}
		if err := client.UploadFile(localPath, remoteGenerated[name]); err != nil {
			return fmt.Errorf("upload %s: %w", name, err)
		}
	}
	servicePath := filepath.Join(repositoryRoot, "packaging", "systemd", "dibra-deploy.service")
	if err := client.UploadFile(servicePath, "/etc/systemd/system/dibra-deploy.service"); err != nil {
		return fmt.Errorf("upload sample service: %w", err)
	}
	if _, err := runRemote(client, "chmod 0755 /usr/local/bin/dibra-deploy /usr/local/bin/dibra-agent /usr/local/bin/dibra-task-server-it /usr/local/bin/dibra-fake-reboot && chmod 0644 /etc/systemd/system/dibra-deploy.service /etc/systemd/system/dibra-deploy.service.d/integration.conf /etc/systemd/system/dibra-task-server-it.service && systemctl daemon-reload && systemctl enable dibra-deploy.service >/dev/null && systemctl start dibra-task-server-it.service"); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runRemote(client, "curl -fsS -o /dev/null http://127.0.0.1:8080/health"); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("task server did not become ready")
}

func uninstallHarness() error {
	client, err := newClient()
	if err != nil {
		return err
	}
	defer client.Close()
	command := "systemctl stop dibra-deploy.service dibra-task-server-it.service >/dev/null 2>&1 || true; " +
		"systemctl disable dibra-deploy.service >/dev/null 2>&1 || true; " +
		"rm -f /etc/systemd/system/dibra-deploy.service /etc/systemd/system/dibra-task-server-it.service /etc/systemd/system/dibra-deploy.service.d/integration.conf; " +
		"rm -f /usr/local/bin/dibra-deploy /usr/local/bin/dibra-agent /usr/local/bin/dibra-task-server-it /usr/local/bin/dibra-fake-reboot; " +
		"rm -rf /etc/systemd/system/dibra-deploy.service.d " + remoteRoot + " /var/lib/dibra-deploy; " +
		"systemctl daemon-reload"
	_, err = runRemote(client, command)
	return err
}

func remoteArchitecture(client *ssh.Client) (string, error) {
	output, err := runRemote(client, "uname -m")
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(output) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported integration container architecture %q", output)
	}
}

func buildLinuxBinary(packagePath, outputPath, architecture string) error {
	command := exec.Command("go", "build", "-o", outputPath, packagePath)
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+architecture, "CGO_ENABLED=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s for linux/%s: %w\n%s", packagePath, architecture, err, output)
	}
	return nil
}

func runRemote(client *ssh.Client, command string) (string, error) {
	stdout, stderr, err := client.Run(command)
	if err != nil {
		return strings.TrimSpace(stdout), fmt.Errorf("remote command %q: %w (stdout=%q stderr=%q)", command, err, stdout, stderr)
	}
	return strings.TrimSpace(stdout), nil
}

func resetHarness(t *testing.T, client *ssh.Client) {
	t.Helper()
	command := "systemctl stop dibra-deploy.service >/dev/null 2>&1 || true; " +
		"find " + remoteQueue + " -mindepth 1 -maxdepth 1 -type f -delete; " +
		": > " + requestLog + "; " +
		"rm -rf " + remoteResult + " /var/lib/dibra-deploy; " +
		"rm -f " + remoteRoot + "/fake-reboot.txt /tmp/dibra-deploy-it-escape; " +
		"systemctl reset-failed dibra-deploy.service >/dev/null 2>&1 || true"
	if _, err := runRemote(client, command); err != nil {
		t.Fatal(err)
	}
}

func startDeploy(t *testing.T, client *ssh.Client) {
	t.Helper()
	if _, err := runRemote(client, "systemctl start dibra-deploy.service"); err != nil {
		t.Fatal(err)
	}
}

func stopDeploy(t *testing.T, client *ssh.Client) {
	t.Helper()
	if _, err := runRemote(client, "systemctl stop dibra-deploy.service"); err != nil {
		t.Fatal(err)
	}
}

func serviceLogs(t *testing.T, client *ssh.Client) string {
	t.Helper()
	invocationID := serviceProperty(t, client, "InvocationID")
	command := "journalctl -u dibra-deploy.service --no-pager -n 300 -o cat"
	if invocationID != "" {
		command = "journalctl _SYSTEMD_INVOCATION_ID=" + shellQuote(invocationID) + " --no-pager -n 300 -o cat"
	}
	output, err := runRemote(client, command)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func serviceProperty(t *testing.T, client *ssh.Client, property string) string {
	t.Helper()
	output, err := runRemote(client, "systemctl show dibra-deploy.service -p "+property+" --value")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(output)
}

func serviceIsActive(client *ssh.Client) bool {
	_, err := runRemote(client, "systemctl is-active --quiet dibra-deploy.service")
	return err == nil
}

func remoteFileExists(client *ssh.Client, path string) bool {
	_, err := runRemote(client, "test -f "+shellQuote(path))
	return err == nil
}

func jobDirectoriesEmpty(client *ssh.Client) bool {
	count, err := runRemote(client, "find /var/lib/dibra-deploy/jobs -mindepth 1 -maxdepth 1 | wc -l")
	return err == nil && strings.TrimSpace(count) == "0"
}

func readRemoteFile(t *testing.T, client *ssh.Client, path string) string {
	t.Helper()
	output, err := runRemote(client, "cat "+shellQuote(path))
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func waitForCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, description)
}

func queueFixture(t *testing.T, client *ssh.Client, name, fixture string, wrapped bool) {
	t.Helper()
	archivePath := createProjectArchive(t, filepath.Join(repositoryRoot, "test", "deploy_integration", "testdata", fixture), wrapped)
	queueArchive(t, client, name, archivePath)
}

func queueArchive(t *testing.T, client *ssh.Client, name, archivePath string) {
	t.Helper()
	remoteUpload := filepath.Join(remoteQueue, name+".upload")
	remoteArchive := filepath.Join(remoteQueue, name+".zip")
	if err := client.UploadFile(archivePath, remoteUpload); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemote(client, "mv "+shellQuote(remoteUpload)+" "+shellQuote(remoteArchive)); err != nil {
		t.Fatal(err)
	}
}

func createProjectArchive(t *testing.T, fixtureRoot string, wrapped bool) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "project.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	prefix := ""
	if wrapped {
		prefix = "project/"
	}
	err = filepath.Walk(fixtureRoot, func(filePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(fixtureRoot, filePath)
		if relErr != nil {
			return relErr
		}
		name := prefix + filepath.ToSlash(relative)
		header, headerErr := zip.FileInfoHeader(info)
		if headerErr != nil {
			return headerErr
		}
		header.Name = name
		header.Method = zip.Deflate
		entry, createErr := writer.CreateHeader(header)
		if createErr != nil {
			return createErr
		}
		input, openErr := os.Open(filePath)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(entry, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err == nil && filepath.Base(fixtureRoot) == "happy" {
		innerArchive := createInnerArchive(t, []byte("unarchived-from-project\n"))
		entry, createErr := writer.Create(prefix + "assets/bundle.zip")
		if createErr != nil {
			err = createErr
		} else {
			_, err = entry.Write(innerArchive)
		}
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func createInnerArchive(t *testing.T, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("inside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func createTraversalArchive(t *testing.T) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "traversal.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../../tmp/dibra-deploy-it-escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("escape"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
