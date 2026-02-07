package builder

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	agentPath  = "cmd/agent"
	binaryName = "dibra-agent"
)

type Builder struct {
	projectRoot string
	cacheDir    string
}

func New(projectRoot string) *Builder {
	cacheDir := filepath.Join(os.TempDir(), "dibra-cache")
	_ = os.MkdirAll(cacheDir, 0755)
	return &Builder{
		projectRoot: projectRoot,
		cacheDir:    cacheDir,
	}
}

func (b *Builder) Build() (string, error) {
	return b.BuildFor("linux", "amd64")
}

func (b *Builder) BuildFor(goos, goarch string) (string, error) {
	hash, err := b.sourceHash()
	if err != nil {
		return "", fmt.Errorf("failed to compute source hash: %w", err)
	}

	cachedBinary := filepath.Join(b.cacheDir, fmt.Sprintf("%s-%s-%s-%s", binaryName, goos, goarch, hash[:12]))
	if _, err := os.Stat(cachedBinary); err == nil {
		return cachedBinary, nil
	}

	outputPath := cachedBinary
	agentSrcPath := filepath.Join(b.projectRoot, agentPath)

	cmd := exec.Command("go", "build", "-o", outputPath, "-ldflags", "-s -w", ".")
	cmd.Dir = agentSrcPath
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to build agent for %s/%s: %w\n%s", goos, goarch, err, output)
	}

	return outputPath, nil
}

func (b *Builder) sourceHash() (string, error) {
	h := sha256.New()

	agentDir := filepath.Join(b.projectRoot, agentPath)
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(agentDir, e.Name()))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}

	modulesDir := filepath.Join(b.projectRoot, "internal", "modules")
	if err := filepath.Walk(modulesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		data, _ := os.ReadFile(path)
		h.Write(data)
		return nil
	}); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
