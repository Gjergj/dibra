package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PrepareComposeProject writes an inline definition to a temporary directory
// or validates an existing project_src. The returned cleanup directory is
// empty when no temporary files were created.
func PrepareComposeProject(args ComposeCommonArgs, filesystem FileSystem, clock Clock) (string, string, error) {
	if filesystem == nil {
		filesystem = OSFileSystem{}
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if len(args.Definition) > 0 {
		directory := filepath.Join(filesystem.TempDir(), fmt.Sprintf("dibra-compose-%s-%d", sanitizeComposeName(args.ProjectName), clock.Now().UnixNano()))
		if err := filesystem.MkdirAll(directory, 0o700); err != nil {
			return "", "", fmt.Errorf("Error writing to %s - %v", directory, err)
		}
		payload, err := yaml.Marshal(args.Definition)
		if err != nil {
			_ = filesystem.RemoveAll(directory)
			return "", "", fmt.Errorf("Error writing to %s - %v", directory, err)
		}
		composeFile := filepath.Join(directory, "compose.yaml")
		if err := filesystem.WriteFile(composeFile, payload, 0o600); err != nil {
			_ = filesystem.RemoveAll(directory)
			return "", "", fmt.Errorf("Error writing to %s - %v", composeFile, err)
		}
		return directory, directory, nil
	}

	info, err := filesystem.Stat(args.ProjectSrc)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("%q is not a directory", args.ProjectSrc)
	}
	if err := CheckComposeFiles(args, filesystem); err != nil {
		return "", "", err
	}
	return args.ProjectSrc, "", nil
}

// CheckComposeFiles verifies that requested Compose files exist, matching the
// pinned community.docker lookup order and check_files_existing default.
func CheckComposeFiles(args ComposeCommonArgs, filesystem FileSystem) error {
	if len(args.Files) > 0 {
		for _, file := range args.Files {
			path := file
			if !filepath.IsAbs(file) {
				path = filepath.Join(args.ProjectSrc, file)
			}
			if _, err := filesystem.Stat(path); err != nil {
				return fmt.Errorf("Cannot find Compose file %q relative to project directory %q", file, args.ProjectSrc)
			}
		}
		return nil
	}
	if args.CheckFilesExisting != nil && !*args.CheckFilesExisting {
		return nil
	}
	for _, name := range ComposeFileNames {
		if _, err := filesystem.Stat(filepath.Join(args.ProjectSrc, name)); err == nil {
			return nil
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("Cannot find Compose file %q relative to project directory %q", name, args.ProjectSrc)
		}
	}
	return fmt.Errorf("%q does not contain compose.yaml, compose.yml, docker-compose.yaml, or docker-compose.yml", args.ProjectSrc)
}

func sanitizeComposeName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, name)
	if cleaned == "" {
		return "project"
	}
	return cleaned
}
