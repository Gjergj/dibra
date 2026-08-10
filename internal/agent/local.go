package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PrepareLocal resolves an agent with the controller's normal policy and
// installs a version-checked runtime copy for local execution.
func (r *Resolver) PrepareLocal(target Target, runtimePath, version string, force bool) (string, error) {
	resolvedPath, err := r.ResolveForTarget(target)
	if err != nil {
		return "", err
	}
	expectedVersion := strings.TrimPrefix(version, "v")

	resolvedVersion, versionErr := CheckLocalAgentVersion(resolvedPath)
	if versionErr != nil || resolvedVersion != expectedVersion {
		if r.opts.Mode != ModeAuto {
			if versionErr != nil {
				return "", fmt.Errorf("failed to check resolved agent version: %w", versionErr)
			}
			return "", fmt.Errorf("resolved agent version %q does not match dibra-deploy version %q", resolvedVersion, expectedVersion)
		}
		if removeErr := os.Remove(resolvedPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", fmt.Errorf("remove invalid cached agent: %w", removeErr)
		}
		resolvedPath, err = r.ResolveForTarget(target)
		if err != nil {
			return "", err
		}
		resolvedVersion, versionErr = CheckLocalAgentVersion(resolvedPath)
		if versionErr != nil {
			return "", fmt.Errorf("failed to check downloaded agent version: %w", versionErr)
		}
		if resolvedVersion != expectedVersion {
			return "", fmt.Errorf("downloaded agent version %q does not match dibra-deploy version %q", resolvedVersion, expectedVersion)
		}
	}

	if !force {
		if installedVersion, installedErr := CheckLocalAgentVersion(runtimePath); installedErr == nil && installedVersion == expectedVersion {
			return runtimePath, nil
		}
	}

	if err := installLocalAgent(resolvedPath, runtimePath); err != nil {
		return "", err
	}
	installedVersion, err := CheckLocalAgentVersion(runtimePath)
	if err != nil {
		return "", fmt.Errorf("failed to verify installed local agent: %w", err)
	}
	if installedVersion != expectedVersion {
		return "", fmt.Errorf("installed agent version %q does not match dibra-deploy version %q", installedVersion, expectedVersion)
	}
	return runtimePath, nil
}

func installLocalAgent(sourcePath, runtimePath string) error {
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o700); err != nil {
		return fmt.Errorf("create local agent directory: %w", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open resolved agent: %w", err)
	}
	defer source.Close()

	temporary, err := os.CreateTemp(filepath.Dir(runtimePath), ".dibra-agent-*")
	if err != nil {
		return fmt.Errorf("create temporary local agent: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, source); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy local agent: %w", err)
	}
	if err := temporary.Chmod(0o755); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("make local agent executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close local agent: %w", err)
	}
	if err := os.Rename(temporaryPath, runtimePath); err != nil {
		return fmt.Errorf("install local agent: %w", err)
	}
	return nil
}
