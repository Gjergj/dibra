package builder

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// The agent imports execution and version directly in addition to the
	// module tree. Hash every local source tree that can change its behavior so
	// a controller never reuses a binary built against stale shared contracts.
	for _, root := range []string{
		agentPath,
		filepath.Join("internal", "execution"),
		filepath.Join("internal", "modules"),
		filepath.Join("internal", "version"),
	} {
		if err := b.hashGoTree(h, root); err != nil {
			return "", err
		}
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := b.hashFile(h, name); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (b *Builder) hashGoTree(destination io.Writer, relativeRoot string) error {
	root := filepath.Join(b.projectRoot, relativeRoot)
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relativePath, err := filepath.Rel(b.projectRoot, path)
		if err != nil {
			return err
		}
		return b.hashFile(destination, relativePath)
	})
}

func (b *Builder) hashFile(destination io.Writer, relativePath string) error {
	data, err := os.ReadFile(filepath.Join(b.projectRoot, relativePath))
	if err != nil {
		return err
	}
	if _, err := io.WriteString(destination, filepath.ToSlash(relativePath)); err != nil {
		return err
	}
	if _, err := destination.Write([]byte{0}); err != nil {
		return err
	}
	if _, err := destination.Write(data); err != nil {
		return err
	}
	_, err = destination.Write([]byte{0})
	return err
}
