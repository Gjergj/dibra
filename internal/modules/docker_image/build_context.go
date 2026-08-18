package docker_image

import (
	"archive/tar"
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/patternmatcher"
)

func buildContextArchive(root, dockerfile string, fileSystem docker.FileSystem) (io.ReadCloser, string, error) {
	absolute, err := fileSystem.Abs(root)
	if err != nil {
		return nil, "", err
	}
	patterns := dockerIgnorePatterns(absolute, fileSystem)
	originalPatterns := append([]string(nil), patterns...)
	effectiveDockerfile := dockerfile
	var injectedDockerfile []byte
	if dockerfile != "" {
		absoluteDockerfile := dockerfile
		if !filepath.IsAbs(absoluteDockerfile) {
			absoluteDockerfile = filepath.Join(absolute, dockerfile)
		}
		relativeDockerfile, relativeErr := filepath.Rel(absolute, absoluteDockerfile)
		if relativeErr != nil || relativeDockerfile == ".." ||
			strings.HasPrefix(relativeDockerfile, ".."+string(filepath.Separator)) {
			injectedDockerfile, err = fileSystem.ReadFile(absoluteDockerfile)
			if err != nil {
				return nil, "", err
			}
			effectiveDockerfile = generatedDockerfileName()
		} else {
			effectiveDockerfile = filepath.ToSlash(relativeDockerfile)
		}
		patterns = append(patterns, "!"+effectiveDockerfile)
	} else {
		effectiveDockerfile = ""
		patterns = append(patterns, "!Dockerfile")
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return nil, "", err
	}
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
			ignored, matchErr := matcher.MatchesOrParentMatches(archiveName)
			if matchErr != nil {
				return matchErr
			}
			if ignored {
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
		if walkErr == nil && injectedDockerfile != nil {
			walkErr = writeInjectedFile(tarWriter, effectiveDockerfile, injectedDockerfile)
			if walkErr == nil {
				ignoreContents := strings.Join(append(originalPatterns, effectiveDockerfile), "\n")
				walkErr = writeInjectedFile(tarWriter, ".dockerignore", []byte(ignoreContents))
			}
		}
		if closeErr := tarWriter.Close(); walkErr == nil {
			walkErr = closeErr
		}
		_ = writer.CloseWithError(walkErr)
	}()
	return reader, effectiveDockerfile, nil
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

func generatedDockerfileName() string {
	var random [20]byte
	if _, err := rand.Read(random[:]); err == nil {
		return ".dockerfile." + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf(".dockerfile.%x", time.Now().UnixNano())
}

func writeInjectedFile(writer *tar.Writer, name string, content []byte) error {
	if err := writer.WriteHeader(&tar.Header{
		Name: name, Mode: 0o600, Size: int64(len(content)), ModTime: time.Now(),
	}); err != nil {
		return err
	}
	_, err := writer.Write(content)
	return err
}
