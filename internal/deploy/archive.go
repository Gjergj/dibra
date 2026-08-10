package deploy

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	manifestName      = "dibra-deploy.yaml"
	maxArchiveEntries = 100_000
	maxExpandedSize   = int64(2 << 30)
)

type Manifest struct {
	Version   int      `yaml:"version"`
	Playbooks []string `yaml:"playbooks"`
}

type Project struct {
	Root     string
	Manifest Manifest
}

func ExtractProject(archivePath, destination string) (Project, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return Project{}, fmt.Errorf("open ZIP archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveEntries {
		return Project{}, fmt.Errorf("ZIP contains %d entries; maximum is %d", len(reader.File), maxArchiveEntries)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return Project{}, fmt.Errorf("create extraction directory: %w", err)
	}

	seen := make(map[string]struct{}, len(reader.File))
	var expanded int64
	for _, entry := range reader.File {
		cleanName, err := safeArchiveName(entry.Name)
		if err != nil {
			return Project{}, err
		}
		if cleanName == "." {
			continue
		}
		if _, exists := seen[cleanName]; exists {
			return Project{}, fmt.Errorf("ZIP contains duplicate entry %q", cleanName)
		}
		seen[cleanName] = struct{}{}

		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return Project{}, fmt.Errorf("ZIP entry %q has unsupported file type", entry.Name)
		}
		if entry.UncompressedSize64 > uint64(maxExpandedSize) || expanded > maxExpandedSize-int64(entry.UncompressedSize64) {
			return Project{}, fmt.Errorf("ZIP expanded size exceeds %d bytes", maxExpandedSize)
		}

		target := filepath.Join(destination, filepath.FromSlash(cleanName))
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return Project{}, fmt.Errorf("create directory %q: %w", cleanName, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Project{}, fmt.Errorf("create parent for %q: %w", cleanName, err)
		}
		input, err := entry.Open()
		if err != nil {
			return Project{}, fmt.Errorf("open ZIP entry %q: %w", cleanName, err)
		}
		fileMode := mode.Perm()
		if fileMode == 0 {
			fileMode = 0o644
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err != nil {
			input.Close()
			return Project{}, fmt.Errorf("create extracted file %q: %w", cleanName, err)
		}
		remaining := maxExpandedSize - expanded
		written, copyErr := io.Copy(output, io.LimitReader(input, remaining+1))
		closeErr := output.Close()
		inputErr := input.Close()
		if copyErr != nil {
			return Project{}, fmt.Errorf("extract %q: %w", cleanName, copyErr)
		}
		if closeErr != nil {
			return Project{}, fmt.Errorf("close extracted file %q: %w", cleanName, closeErr)
		}
		if inputErr != nil {
			return Project{}, fmt.Errorf("close ZIP entry %q: %w", cleanName, inputErr)
		}
		if written > remaining {
			return Project{}, fmt.Errorf("ZIP expanded size exceeds %d bytes", maxExpandedSize)
		}
		expanded += written
	}

	projectRoot, manifestPath, err := locateManifest(destination)
	if err != nil {
		return Project{}, err
	}
	manifest, err := loadManifest(manifestPath, projectRoot)
	if err != nil {
		return Project{}, err
	}
	return Project{Root: projectRoot, Manifest: manifest}, nil
}

func safeArchiveName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", fmt.Errorf("ZIP contains invalid entry name %q", name)
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("ZIP entry %q contains path traversal", name)
		}
	}
	cleanName := path.Clean(name)
	if path.IsAbs(name) || hasWindowsDrivePrefix(name) || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", fmt.Errorf("ZIP entry %q escapes the project root", name)
	}
	return cleanName, nil
}

func hasWindowsDrivePrefix(name string) bool {
	return len(name) >= 2 && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':'
}

func locateManifest(destination string) (string, string, error) {
	var manifests []string
	err := filepath.WalkDir(destination, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == manifestName {
			manifests = append(manifests, filePath)
		}
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("scan extracted project: %w", err)
	}
	if len(manifests) != 1 {
		return "", "", fmt.Errorf("ZIP must contain exactly one %s; found %d", manifestName, len(manifests))
	}
	relative, err := filepath.Rel(destination, manifests[0])
	if err != nil {
		return "", "", fmt.Errorf("resolve manifest path: %w", err)
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	switch len(parts) {
	case 1:
		return destination, manifests[0], nil
	case 2:
		entries, readErr := os.ReadDir(destination)
		if readErr != nil {
			return "", "", fmt.Errorf("inspect ZIP root: %w", readErr)
		}
		if len(entries) != 1 || !entries[0].IsDir() || entries[0].Name() != parts[0] {
			return "", "", fmt.Errorf("manifest may be enclosed only by one top-level project directory")
		}
		return filepath.Join(destination, parts[0]), manifests[0], nil
	default:
		return "", "", fmt.Errorf("%s must be at the ZIP root or in one enclosing directory", manifestName)
	}
}

func loadManifest(manifestPath, projectRoot string) (Manifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("read deployment manifest: %w", err)
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse deployment manifest: %w", err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("parse deployment manifest: multiple YAML documents are not allowed")
		}
		return Manifest{}, fmt.Errorf("parse deployment manifest: %w", err)
	}
	if manifest.Version != 1 {
		return Manifest{}, fmt.Errorf("unsupported deployment manifest version %d", manifest.Version)
	}
	if len(manifest.Playbooks) == 0 {
		return Manifest{}, fmt.Errorf("deployment manifest must list at least one playbook")
	}
	seen := make(map[string]struct{}, len(manifest.Playbooks))
	for index, playbook := range manifest.Playbooks {
		cleanName, err := safeProjectPath(playbook)
		if err != nil {
			return Manifest{}, fmt.Errorf("playbooks[%d]: %w", index, err)
		}
		extension := strings.ToLower(filepath.Ext(cleanName))
		if extension != ".yaml" && extension != ".yml" {
			return Manifest{}, fmt.Errorf("playbook %q must use a .yaml or .yml extension", playbook)
		}
		if _, exists := seen[cleanName]; exists {
			return Manifest{}, fmt.Errorf("playbook %q appears more than once", playbook)
		}
		seen[cleanName] = struct{}{}
		fullPath := filepath.Join(projectRoot, filepath.FromSlash(cleanName))
		info, statErr := os.Stat(fullPath)
		if statErr != nil {
			return Manifest{}, fmt.Errorf("playbook %q: %w", playbook, statErr)
		}
		if !info.Mode().IsRegular() {
			return Manifest{}, fmt.Errorf("playbook %q is not a regular file", playbook)
		}
		manifest.Playbooks[index] = cleanName
	}
	return manifest, nil
}

func safeProjectPath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") {
		return "", fmt.Errorf("path is empty or invalid")
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("path %q contains traversal", name)
		}
	}
	cleanName := path.Clean(name)
	if path.IsAbs(name) || hasWindowsDrivePrefix(name) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", fmt.Errorf("path %q escapes the project root", name)
	}
	return cleanName, nil
}
