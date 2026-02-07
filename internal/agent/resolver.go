package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gjergjiramku/dibra/internal/builder"
	"github.com/gjergjiramku/dibra/internal/ssh"
)

type Mode int

const (
	ModeAuto  Mode = iota // download from GitHub Releases
	ModeBuild             // build from source
	ModePath              // use explicit path
)

type Target struct {
	OS   string // linux, darwin
	Arch string // amd64, arm64
}

type Options struct {
	Mode        Mode
	AgentPath   string // for ModePath
	Version     string // controller version
	ProjectRoot string // for ModeBuild
}

type Resolver struct {
	opts Options
}

func NewResolver(opts Options) *Resolver {
	return &Resolver{opts: opts}
}

func (r *Resolver) Resolve(client *ssh.Client) (string, error) {
	switch r.opts.Mode {
	case ModePath:
		if _, err := os.Stat(r.opts.AgentPath); err != nil {
			return "", fmt.Errorf("agent binary not found at %s: %w", r.opts.AgentPath, err)
		}
		return r.opts.AgentPath, nil

	case ModeBuild:
		target, err := detectRemoteTarget(client)
		if err != nil {
			return "", fmt.Errorf("failed to detect remote OS/arch: %w", err)
		}
		b := builder.New(r.opts.ProjectRoot)
		return b.BuildFor(target.OS, target.Arch)

	default:
		norm, _, err := normalizeVersion(r.opts.Version)
		if err != nil {
			return "", fmt.Errorf("cannot auto-download agent: controller was built with version %q (no GitHub Release exists).\n"+
				"Use --agent-build to build the agent from source, or install a released dibra controller binary.", r.opts.Version)
		}

		target, err := detectRemoteTarget(client)
		if err != nil {
			return "", fmt.Errorf("failed to detect remote OS/arch: %w", err)
		}

		return r.resolveFromCache(norm, target)
	}
}

func (r *Resolver) resolveFromCache(version string, target Target) (string, error) {
	cacheDir := defaultCacheDir()
	binaryPath := cachedBinaryPath(cacheDir, version, target)

	if _, err := os.Stat(binaryPath); err == nil {
		return binaryPath, nil
	}

	archivePath := cachedArchivePath(cacheDir, version, target)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		url := downloadURL(version, target)
		fmt.Printf("Downloading agent from %s...\n", url)
		if err := downloadFile(url, archivePath); err != nil {
			return "", fmt.Errorf("failed to download agent from %s: %w\n"+
				"Use --agent-build to build from source, or --agent-path to specify a local binary.", url, err)
		}
	}

	fmt.Println("Extracting agent binary...")
	if err := extractAgent(archivePath, binaryPath); err != nil {
		os.Remove(archivePath)
		return "", fmt.Errorf("failed to extract agent: %w", err)
	}

	return binaryPath, nil
}

func normalizeVersion(v string) (norm string, tag string, err error) {
	if v == "" || v == "dev" {
		return "", "", fmt.Errorf("dev version")
	}
	norm = strings.TrimPrefix(v, "v")
	tag = "v" + norm
	return norm, tag, nil
}

func defaultCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".dibra", "cache", "agents")
}

func cachedBinaryPath(cacheDir, version string, target Target) string {
	return filepath.Join(cacheDir, version, fmt.Sprintf("dibra-agent_%s_%s", target.OS, target.Arch))
}

func cachedArchivePath(cacheDir, version string, target Target) string {
	return filepath.Join(cacheDir, version, fmt.Sprintf("dibra-agent_%s_%s_%s.tar.gz", version, target.OS, target.Arch))
}

func downloadURL(version string, target Target) string {
	return fmt.Sprintf(
		"https://github.com/Gjergj/dibra/releases/download/v%s/dibra-agent_%s_%s_%s.tar.gz",
		version, version, target.OS, target.Arch,
	)
}
