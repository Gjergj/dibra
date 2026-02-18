package cueconfig

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gjergjiramku/dibra/internal/version"
)

type SchemaInstallResult struct {
	Path  string
	Files int
	Version string
}

type SchemaStatus struct {
	Path      string
	Files     int
	Installed bool
	Version   string
	Current   string
	UpToDate  bool
}

// InstallSchema writes the embedded dibra schema package into cue.mod/pkg so
// external tooling (like the CUE language server) can autocomplete it.
func InstallSchema(root string, force bool) (SchemaInstallResult, error) {
	moduleRoot := findModuleRoot(root)
	if moduleRoot == "" {
		return SchemaInstallResult{}, fmt.Errorf("no cue.mod directory found from %s", root)
	}

        destDir := filepath.Join(moduleRoot, "cue.mod", "pkg", "dibra.dev", "schema")
        if err := os.MkdirAll(destDir, 0755); err != nil {
                return SchemaInstallResult{}, fmt.Errorf("failed to create schema dir: %w", err)
        }

        entries, err := fs.ReadDir(schemaFS, "schema")
        if err != nil {
                return SchemaInstallResult{}, fmt.Errorf("failed to read embedded schema: %w", err)
        }

        written := 0
        for _, entry := range entries {
                if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cue") {
                        continue
                }
                data, err := schemaFS.ReadFile(filepath.Join("schema", entry.Name()))
                if err != nil {
                        return SchemaInstallResult{}, fmt.Errorf("failed to read schema %s: %w", entry.Name(), err)
                }

                destPath := filepath.Join(destDir, entry.Name())
                if err := writeSchemaFile(destPath, data, force); err != nil {
                        return SchemaInstallResult{}, err
                }
                if _, err := os.Stat(destPath); err == nil {
                        written++
                }
        }

	if err := writeSchemaVersion(destDir, version.Version, force); err != nil {
		return SchemaInstallResult{}, err
	}

	return SchemaInstallResult{Path: destDir, Files: written, Version: version.Version}, nil
}

// SchemaStatus reports whether the schema is installed under cue.mod/pkg.
func GetSchemaStatus(root string) (SchemaStatus, error) {
	moduleRoot := findModuleRoot(root)
	if moduleRoot == "" {
		return SchemaStatus{Path: filepath.Join(root, "cue.mod", "pkg", "dibra.dev", "schema"), Current: version.Version}, nil
	}

	destDir := filepath.Join(moduleRoot, "cue.mod", "pkg", "dibra.dev", "schema")
	entries, err := os.ReadDir(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return SchemaStatus{Path: destDir, Current: version.Version}, nil
		}
		return SchemaStatus{}, fmt.Errorf("failed to read schema dir: %w", err)
	}

	files := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cue") {
			continue
		}
		files++
	}

	installedVersion, _ := readSchemaVersion(destDir)

	status := SchemaStatus{
		Path:      destDir,
		Files:     files,
		Installed: files > 0,
		Version:   installedVersion,
		Current:   version.Version,
	}
	status.UpToDate = status.Installed && status.Version != "" && status.Version == status.Current
	return status, nil
}

func writeSchemaVersion(dir, ver string, force bool) error {
	if ver == "" {
		return nil
	}
	path := filepath.Join(dir, "schema.version")
	data := []byte(ver + "\n")
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		if os.IsExist(err) && !force {
			return nil
		}
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func readSchemaVersion(dir string) (string, error) {
	path := filepath.Join(dir, "schema.version")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func writeSchemaFile(path string, data []byte, force bool) error {
        flags := os.O_WRONLY | os.O_CREATE
        if force {
                flags |= os.O_TRUNC
        } else {
                flags |= os.O_EXCL
        }

        f, err := os.OpenFile(path, flags, 0644)
        if err != nil {
                if os.IsExist(err) && !force {
                        return nil
                }
                return fmt.Errorf("failed to write %s: %w", path, err)
        }
        defer f.Close()

        if _, err := f.Write(data); err != nil {
                return fmt.Errorf("failed to write %s: %w", path, err)
        }
        return nil
}
