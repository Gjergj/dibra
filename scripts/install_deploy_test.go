package scripts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const sampleUnit = `[Unit]
Description=Dibra local pull deployment runner

[Service]
ExecStart=/usr/local/bin/dibra-deploy

[Install]
WantedBy=multi-user.target
`

const sampleProjectJWT = "header.payload.signature"

func TestInstallDeployInstallsReleaseWithoutEnablingService(t *testing.T) {
	fixture := newInstallerFixture(t, "v1.2.3", false)

	output, err := fixture.run("--version", "v1.2.3")
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}

	assertInstalledFiles(t, fixture)
	log := readFile(t, fixture.systemctlLog)
	if log != "daemon-reload\n" {
		t.Fatalf("unexpected systemctl calls:\n%s", log)
	}
	if !strings.Contains(output, "installed without enabling the service") {
		t.Fatalf("installer did not explain the disabled default:\n%s", output)
	}
}

func TestInstallDeployFindsLatestReleaseAndEnablesService(t *testing.T) {
	fixture := newInstallerFixture(t, "v2.4.0", false)

	output, err := fixture.run("--enable")
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}

	assertInstalledFiles(t, fixture)
	log := readFile(t, fixture.systemctlLog)
	want := "daemon-reload\nenable --now dibra-deploy.service\n"
	if log != want {
		t.Fatalf("unexpected systemctl calls:\nwant:\n%s\ngot:\n%s", want, log)
	}
}

func TestInstallDeployRejectsChecksumMismatch(t *testing.T) {
	fixture := newInstallerFixture(t, "v1.2.3", true)

	output, err := fixture.run("--version", "v1.2.3")
	if err == nil {
		t.Fatalf("installer unexpectedly accepted an invalid checksum:\n%s", output)
	}
	if !strings.Contains(output, "checksum verification failed") {
		t.Fatalf("unexpected installer error:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(fixture.installDir, "dibra-deploy")); !os.IsNotExist(statErr) {
		t.Fatalf("binary was installed after checksum failure: %v", statErr)
	}
}

func TestInstallDeployRequiresProjectJWT(t *testing.T) {
	fixture := newInstallerFixture(t, "v1.2.3", false)
	output, err := fixture.runWithoutToken("--version", "v1.2.3")
	if err == nil || !strings.Contains(output, "PROJECT_JWT is required") {
		t.Fatalf("installer accepted a missing token: err=%v output=%s", err, output)
	}
}

type installerFixture struct {
	t              *testing.T
	root           string
	installDir     string
	unitDir        string
	configDir      string
	fakeBin        string
	releaseBaseURL string
	latestURL      string
	systemctl      string
	systemctlLog   string
}

func newInstallerFixture(t *testing.T, tag string, badChecksum bool) *installerFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer is a POSIX shell script")
	}

	root := t.TempDir()
	fakeBin := filepath.Join(root, "fake-bin")
	releaseRoot := filepath.Join(root, "releases")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "$1" in
    -s) echo Linux ;;
    -m) echo x86_64 ;;
    *) exit 1 ;;
esac
`)
	systemctlLog := filepath.Join(root, "systemctl.log")
	systemctl := filepath.Join(fakeBin, "systemctl")
	writeExecutable(t, systemctl, `#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
`)

	createRelease(t, releaseRoot, tag, badChecksum)
	latestPath := filepath.Join(root, "latest.json")
	if err := os.WriteFile(latestPath, []byte(fmt.Sprintf("{\"tag_name\":%q}\n", tag)), 0o644); err != nil {
		t.Fatal(err)
	}

	return &installerFixture{
		t:              t,
		root:           root,
		installDir:     filepath.Join(root, "installed", "bin"),
		unitDir:        filepath.Join(root, "installed", "systemd"),
		configDir:      filepath.Join(root, "installed", "config"),
		fakeBin:        fakeBin,
		releaseBaseURL: fileURL(releaseRoot),
		latestURL:      fileURL(latestPath),
		systemctl:      systemctl,
		systemctlLog:   systemctlLog,
	}
}

func (f *installerFixture) run(args ...string) (string, error) {
	return f.runWithToken(sampleProjectJWT, args...)
}

func (f *installerFixture) runWithoutToken(args ...string) (string, error) {
	return f.runWithToken("", args...)
}

func (f *installerFixture) runWithToken(token string, args ...string) (string, error) {
	f.t.Helper()
	script, err := filepath.Abs("install-dibra-deploy.sh")
	if err != nil {
		f.t.Fatal(err)
	}
	basePath := os.Getenv("PATH")
	env := environmentWith(map[string]string{
		"DIBRA_DEPLOY_TOKEN":              "",
		"DIBRA_DEPLOY_LATEST_RELEASE_URL": f.latestURL,
		"DIBRA_DEPLOY_NO_SUDO":            "1",
		"DIBRA_DEPLOY_RELEASE_BASE_URL":   f.releaseBaseURL,
		"PATH":                            f.fakeBin + string(os.PathListSeparator) + basePath,
		"SYSTEMCTL":                       f.systemctl,
		"SYSTEMCTL_LOG":                   f.systemctlLog,
		"VERSION":                         "",
	})
	installArgs := []string{
		script,
		"--install-dir", f.installDir,
		"--unit-dir", f.unitDir,
		"--config-dir", f.configDir,
	}
	if token != "" {
		installArgs = append(installArgs, token)
	}
	installArgs = append(installArgs, args...)
	cmd := exec.Command("sh", installArgs...)
	cmd.Env = env
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func createRelease(t *testing.T, releaseRoot, tag string, badChecksum bool) {
	t.Helper()
	version := strings.TrimPrefix(tag, "v")
	filename := "dibra-deploy_" + version + "_linux_amd64.tar.gz"
	releaseDir := filepath.Join(releaseRoot, tag)
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(releaseDir, filename)
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	gzipWriter := gzip.NewWriter(io.MultiWriter(archive, hash))
	tarWriter := tar.NewWriter(gzipWriter)
	entries := []struct {
		name string
		mode int64
		body string
	}{
		{
			name: "dibra-deploy",
			mode: 0o755,
			body: "#!/bin/sh\necho dibra-deploy " + version + "\n",
		},
		{name: "dibra-deploy.service", mode: 0o644, body: sampleUnit},
	}
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name,
			Mode: entry.mode,
			Size: int64(len(entry.body)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tarWriter, entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	if badChecksum {
		checksum = strings.Repeat("0", sha256.Size*2)
	}
	checksums := checksum + "  " + filename + "\n"
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertInstalledFiles(t *testing.T, fixture *installerFixture) {
	t.Helper()
	binaryPath := filepath.Join(fixture.installDir, "dibra-deploy")
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode = %o, want 755", info.Mode().Perm())
	}

	unitPath := filepath.Join(fixture.unitDir, "dibra-deploy.service")
	unitInfo, err := os.Stat(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if unitInfo.Mode().Perm() != 0o644 {
		t.Fatalf("unit mode = %o, want 644", unitInfo.Mode().Perm())
	}
	unit := readFile(t, unitPath)
	wantExecStart := "ExecStart=" + binaryPath
	if !strings.Contains(unit, wantExecStart+"\n") {
		t.Fatalf("unit does not contain %q:\n%s", wantExecStart, unit)
	}
	wantEnvironment := "EnvironmentFile=" + filepath.Join(fixture.configDir, "environment")
	if !strings.Contains(unit, wantEnvironment+"\n") {
		t.Fatalf("unit does not contain %q:\n%s", wantEnvironment, unit)
	}
	environmentPath := filepath.Join(fixture.configDir, "environment")
	environmentInfo, err := os.Stat(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	if environmentInfo.Mode().Perm() != 0o600 {
		t.Fatalf("environment mode = %o, want 600", environmentInfo.Mode().Perm())
	}
	wantConfig := "DIBRA_DEPLOY_TOKEN=" + sampleProjectJWT + "\nDIBRA_DEPLOY_ENDPOINT=http://localhost:8080/gettasks\n"
	if config := readFile(t, environmentPath); config != wantConfig {
		t.Fatalf("environment file = %q, want %q", config, wantConfig)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}

func environmentWith(overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[name]; !overridden {
			env = append(env, entry)
		}
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}
