package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunDeployDockerHostRequiresProjectToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Docker-host runner is a Bash script")
	}
	script, err := filepath.Abs("run-deploy-docker-host.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Env = environmentWith(map[string]string{
		"DIBRA_DEPLOY_ENV_FILE": filepath.Join(t.TempDir(), "missing.env"),
		"DIBRA_DEPLOY_TOKEN":    "",
	})
	output, runErr := cmd.CombinedOutput()
	if runErr == nil || !strings.Contains(string(output), "DIBRA_DEPLOY_TOKEN is required") {
		t.Fatalf("runner accepted a missing token: err=%v output=%s", runErr, output)
	}
}

func TestRunDeployDockerHostLoadsAndForwardsProjectToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Docker-host runner is a Bash script")
	}
	root := t.TempDir()
	fakeBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(root, "docker.log")
	writeExecutable(t, filepath.Join(fakeBin, "docker"), `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$*" in
  *"exec -T testhost uname -m"*) printf '%s\n' x86_64 ;;
esac
`)
	writeExecutable(t, filepath.Join(fakeBin, "go"), `#!/bin/sh
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    output=$1
  fi
  shift
done
[ -n "$output" ] || exit 1
: > "$output"
chmod 0755 "$output"
`)
	envFile := filepath.Join(root, ".env.deploy")
	if err := os.WriteFile(envFile, []byte("DIBRA_DEPLOY_TOKEN="+sampleProjectJWT+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("run-deploy-docker-host.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Env = environmentWith(map[string]string{
		"DIBRA_BUILD_DATE":      "2026-08-21T00:00:00Z",
		"DIBRA_COMMIT":          "test",
		"DIBRA_DEPLOY_ENV_FILE": envFile,
		"DIBRA_DEPLOY_TOKEN":    "",
		"DIBRA_VERSION":         "test",
		"DOCKER_LOG":            dockerLog,
		"PATH":                  fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("runner failed: %v\n%s", runErr, output)
	}
	log := readFile(t, dockerLog)
	want := "exec -e DIBRA_DEPLOY_TOKEN=" + sampleProjectJWT + " testhost /usr/local/bin/dibra-deploy"
	if !strings.Contains(log, want) {
		t.Fatalf("project token was not forwarded to dibra-deploy:\n%s", log)
	}
	if strings.Contains(string(output), sampleProjectJWT) {
		t.Fatalf("runner printed the project token:\n%s", output)
	}
}
