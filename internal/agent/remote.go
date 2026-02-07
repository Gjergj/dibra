package agent

import (
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

func detectRemoteTarget(client *ssh.Client) (Target, error) {
	osName, _, err := client.Run("uname -s")
	if err != nil {
		return Target{}, fmt.Errorf("failed to run uname -s: %w", err)
	}

	archName, _, err := client.Run("uname -m")
	if err != nil {
		return Target{}, fmt.Errorf("failed to run uname -m: %w", err)
	}

	goos, err := mapOS(strings.TrimSpace(osName))
	if err != nil {
		return Target{}, err
	}

	goarch, err := mapArch(strings.TrimSpace(archName))
	if err != nil {
		return Target{}, err
	}

	return Target{OS: goos, Arch: goarch}, nil
}

func mapOS(uname string) (string, error) {
	switch uname {
	case "Linux":
		return "linux", nil
	case "Darwin":
		return "darwin", nil
	default:
		return "", fmt.Errorf("unsupported remote OS: %q (uname -s returned %q)", uname, uname)
	}
}

func mapArch(uname string) (string, error) {
	switch uname {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported remote architecture: %q (uname -m returned %q)", uname, uname)
	}
}

func CheckRemoteAgentVersion(client *ssh.Client, agentPath string) (string, error) {
	stdout, _, err := client.Run(fmt.Sprintf("%s --version", agentPath))
	if err != nil {
		return "", err
	}
	output := strings.TrimSpace(stdout)
	parts := strings.Fields(output)
	if len(parts) >= 2 {
		return strings.TrimPrefix(parts[1], "v"), nil
	}
	return output, nil
}
