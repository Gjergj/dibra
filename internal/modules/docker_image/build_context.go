package docker_image

import (
	"archive/tar"
	"bufio"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

func buildContextArchive(root string, fileSystem docker.FileSystem) (io.ReadCloser, error) {
	absolute, err := fileSystem.Abs(root)
	if err != nil {
		return nil, err
	}
	patterns := dockerIgnorePatterns(absolute, fileSystem)
	reader, writer := io.Pipe()
	go func() {
		tarWriter := tar.NewWriter(writer)
		walkErr := fileSystem.WalkDir(absolute, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if current == absolute {
				return nil
			}
			relative, err := filepath.Rel(absolute, current)
			if err != nil {
				return err
			}
			archiveName := filepath.ToSlash(relative)
			if ignoredByDocker(archiveName, entry.IsDir(), patterns) &&
				archiveName != ".dockerignore" && path.Base(archiveName) != "Dockerfile" {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			linkTarget := ""
			if info.Mode()&fs.ModeSymlink != 0 {
				linkTarget, err = fileSystem.Readlink(current)
				if err != nil {
					return err
				}
			}
			header, err := tar.FileInfoHeader(info, linkTarget)
			if err != nil {
				return err
			}
			header.Name = archiveName
			if entry.IsDir() {
				header.Name += "/"
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			file, err := fileSystem.Open(current)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tarWriter, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		})
		if closeErr := tarWriter.Close(); walkErr == nil {
			walkErr = closeErr
		}
		_ = writer.CloseWithError(walkErr)
	}()
	return reader, nil
}

func dockerIgnorePatterns(root string, fileSystem docker.FileSystem) []string {
	data, err := fileSystem.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		return nil
	}
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		pattern := strings.TrimSpace(scanner.Text())
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		pattern = strings.TrimPrefix(pattern, "./")
		pattern = strings.TrimPrefix(pattern, "/")
		result = append(result, pattern)
	}
	return result
}

func ignoredByDocker(name string, directory bool, patterns []string) bool {
	ignored := false
	for _, raw := range patterns {
		negated := strings.HasPrefix(raw, "!")
		pattern := strings.TrimPrefix(raw, "!")
		pattern = strings.TrimSuffix(pattern, "/")
		matched, _ := path.Match(pattern, name)
		if !matched && !strings.Contains(pattern, "/") {
			for _, component := range strings.Split(name, "/") {
				if componentMatched, _ := path.Match(pattern, component); componentMatched {
					matched = true
					break
				}
			}
		}
		if !matched && directory {
			matched = strings.HasPrefix(name+"/", pattern+"/")
		}
		if matched {
			ignored = !negated
		}
	}
	return ignored
}
