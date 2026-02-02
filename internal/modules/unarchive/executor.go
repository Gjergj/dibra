package unarchive

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func Execute(req Request) Response {
	if req.Dest == "" {
		return Response{Failed: true, Msg: "dest is required"}
	}

	if req.Src == "" {
		return Response{Failed: true, Msg: "src is required"}
	}

	if len(req.Exclude) > 0 && len(req.Include) > 0 {
		return Response{Failed: true, Msg: "exclude and include are mutually exclusive"}
	}

	destInfo, err := os.Stat(req.Dest)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("dest does not exist: %s", req.Dest)}
	}
	if !destInfo.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("dest is not a directory: %s", req.Dest)}
	}

	srcInfo, err := os.Stat(req.Src)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("src does not exist: %s", req.Src)}
	}
	if srcInfo.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("src is a directory, expected an archive: %s", req.Src)}
	}

	if req.Checksum != "" {
		srcChecksum, err := sha1File(req.Src)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to compute src checksum: %v", err)}
		}
		if srcChecksum != req.Checksum {
			return Response{Failed: true, Msg: fmt.Sprintf("checksum mismatch: expected %s, got %s", req.Checksum, srcChecksum)}
		}
	}

	if req.Creates != "" {
		if _, err := os.Stat(req.Creates); err == nil {
			return Response{
				Changed: false,
				Dest:    req.Dest,
				Src:     req.Src,
				Msg:     fmt.Sprintf("skipped, creates path exists: %s", req.Creates),
			}
		}
	}

	archiveType := detectArchiveType(req.Src)
	if archiveType == "" {
		return Response{Failed: true, Msg: fmt.Sprintf("unsupported archive format: %s", req.Src)}
	}

	var handler archiveHandler
	var handlerName string

	if archiveType == "zip" {
		h, err := newZipHandler(req.Src, req.Dest, req.Exclude, req.Include, req.KeepNewer, req.ExtraOpts)
		if err != nil {
			return Response{Failed: true, Msg: err.Error()}
		}
		handler = h
		handlerName = "zip"
	} else {
		h, err := newTarHandler(req.Src, req.Dest, archiveType, req.Exclude, req.Include, req.KeepNewer, req.ExtraOpts)
		if err != nil {
			return Response{Failed: true, Msg: err.Error()}
		}
		handler = h
		handlerName = "tar"
	}

	var files []string
	if req.ListFiles {
		var err error
		files, err = handler.listFiles()
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to list archive contents: %v", err)}
		}
	}

	unarchived, err := handler.isUnarchived()
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to check if already unarchived: %v", err)}
	}

	if unarchived {
		return Response{
			Changed: false,
			Dest:    req.Dest,
			Src:     req.Src,
			Files:   files,
			Handler: handlerName,
			Msg:     "already unarchived",
		}
	}

	if err := handler.extract(); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("extraction failed: %v", err)}
	}

	if req.Owner != "" || req.Group != "" || req.Mode != "" {
		if err := applyAttributesRecursive(req.Dest, req.Owner, req.Group, req.Mode); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
		}
	}

	return Response{
		Changed: true,
		Dest:    req.Dest,
		Src:     req.Src,
		Files:   files,
		Handler: handlerName,
		Msg:     "archive extracted",
	}
}

type archiveHandler interface {
	isUnarchived() (bool, error)
	listFiles() ([]string, error)
	extract() error
}

func detectArchiveType(src string) string {
	lower := strings.ToLower(src)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return "tar-gz"
	case strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2"):
		return "tar-bz2"
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		return "tar-xz"
	case strings.HasSuffix(lower, ".tar.zst"):
		return "tar-zst"
	case strings.HasSuffix(lower, ".tar"):
		return "tar"
	default:
		return ""
	}
}

type tarHandler struct {
	src         string
	dest        string
	archiveType string
	exclude     []string
	include     []string
	keepNewer   bool
	extraOpts   []string
	tarPath     string
}

func newTarHandler(src, dest, archiveType string, exclude, include []string, keepNewer bool, extraOpts []string) (*tarHandler, error) {
	tarPath, err := exec.LookPath("tar")
	if err != nil {
		return nil, fmt.Errorf("tar binary not found: %v", err)
	}

	if archiveType == "tar-zst" {
		if _, err := exec.LookPath("zstd"); err != nil {
			return nil, fmt.Errorf("zstd binary not found (required for .tar.zst): %v", err)
		}
	}

	return &tarHandler{
		src:         src,
		dest:        dest,
		archiveType: archiveType,
		exclude:     exclude,
		include:     include,
		keepNewer:   keepNewer,
		extraOpts:   extraOpts,
		tarPath:     tarPath,
	}, nil
}

func (h *tarHandler) compressionFlag() []string {
	switch h.archiveType {
	case "tar-gz":
		return []string{"-z"}
	case "tar-bz2":
		return []string{"-j"}
	case "tar-xz":
		return []string{"-J"}
	case "tar-zst":
		return []string{"--use-compress-program=zstd"}
	default:
		return nil
	}
}

func (h *tarHandler) isUnarchived() (bool, error) {
	args := []string{"--diff"}
	args = append(args, h.compressionFlag()...)
	args = append(args, "-C", h.dest, "-f", h.src)

	cmd := exec.Command(h.tarPath, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.CombinedOutput()

	if err == nil {
		return true, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		if exitErr.ExitCode() == 2 {
			if strings.Contains(string(output), "No such file or directory") ||
				strings.Contains(string(output), "Not found in archive") {
				return false, nil
			}
		}
	}

	return false, nil
}

func (h *tarHandler) listFiles() ([]string, error) {
	args := []string{"-tf", h.src}
	args = append(args, h.compressionFlag()...)

	cmd := exec.Command(h.tarPath, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tar list failed: %v", err)
	}

	var files []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

func (h *tarHandler) extract() error {
	args := []string{"-x"}
	args = append(args, h.compressionFlag()...)
	args = append(args, "-f", h.src, "-C", h.dest)

	if h.keepNewer {
		args = append(args, "--keep-newer-files")
	}

	for _, pattern := range h.exclude {
		args = append(args, "--exclude="+pattern)
	}

	for _, pattern := range h.include {
		args = append(args, pattern)
	}

	args = append(args, h.extraOpts...)

	cmd := exec.Command(h.tarPath, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar extraction failed: %v, output: %s", err, output)
	}
	return nil
}

type zipHandler struct {
	src       string
	dest      string
	exclude   []string
	include   []string
	keepNewer bool
	extraOpts []string
	unzipPath string
}

func newZipHandler(src, dest string, exclude, include []string, keepNewer bool, extraOpts []string) (*zipHandler, error) {
	unzipPath, err := exec.LookPath("unzip")
	if err != nil {
		return nil, fmt.Errorf("unzip binary not found: %v", err)
	}

	return &zipHandler{
		src:       src,
		dest:      dest,
		exclude:   exclude,
		include:   include,
		keepNewer: keepNewer,
		extraOpts: extraOpts,
		unzipPath: unzipPath,
	}, nil
}

func (h *zipHandler) isUnarchived() (bool, error) {
	zipinfoPath, err := exec.LookPath("zipinfo")
	if err != nil {
		return false, nil
	}

	cmd := exec.Command(zipinfoPath, "-1", h.src)
	output, err := cmd.Output()
	if err != nil {
		return false, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" {
			continue
		}

		if strings.HasSuffix(entry, "/") {
			continue
		}

		destPath := filepath.Join(h.dest, entry)
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			return false, nil
		}
	}

	return true, nil
}

func (h *zipHandler) listFiles() ([]string, error) {
	zipinfoPath, err := exec.LookPath("zipinfo")
	if err != nil {
		cmd := exec.Command(h.unzipPath, "-l", h.src)
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("unzip list failed: %v", err)
		}

		var files []string
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		inList := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "--------") {
				inList = !inList
				continue
			}
			if inList {
				fields := strings.Fields(line)
				if len(fields) >= 4 {
					files = append(files, fields[len(fields)-1])
				}
			}
		}
		return files, nil
	}

	cmd := exec.Command(zipinfoPath, "-1", h.src)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("zipinfo failed: %v", err)
	}

	var files []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

func (h *zipHandler) extract() error {
	args := []string{"-o"}

	if h.keepNewer {
		args = append(args, "-u")
	}

	args = append(args, h.src, "-d", h.dest)

	for _, pattern := range h.include {
		args = append(args, pattern)
	}

	for _, pattern := range h.exclude {
		args = append(args, "-x", pattern)
	}

	args = append(args, h.extraOpts...)

	cmd := exec.Command(h.unzipPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("unzip failed: %v, output: %s", err, output)
	}
	return nil
}

func applyAttributesRecursive(root, owner, group, mode string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return setAttributes(path, owner, group, mode)
	})
}

func setAttributes(path, owner, group, mode string) error {
	if owner != "" {
		uid, err := lookupUID(owner)
		if err != nil {
			return fmt.Errorf("failed to lookup owner %s: %w", owner, err)
		}

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get file stat")
		}

		if int(stat.Uid) != uid {
			if err := os.Chown(path, uid, -1); err != nil {
				return fmt.Errorf("failed to chown: %w", err)
			}
		}
	}

	if group != "" {
		gid, err := lookupGID(group)
		if err != nil {
			return fmt.Errorf("failed to lookup group %s: %w", group, err)
		}

		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get file stat")
		}

		if int(stat.Gid) != gid {
			if err := os.Chown(path, -1, gid); err != nil {
				return fmt.Errorf("failed to chgrp: %w", err)
			}
		}
	}

	if mode != "" {
		newMode, err := parseMode(mode)
		if err != nil {
			return fmt.Errorf("invalid mode %s: %w", mode, err)
		}

		if err := os.Chmod(path, newMode); err != nil {
			return fmt.Errorf("failed to chmod: %w", err)
		}
	}

	return nil
}

func parseMode(mode string) (os.FileMode, error) {
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(m), nil
}

func lookupUID(name string) (int, error) {
	if uid, err := strconv.Atoi(name); err == nil {
		return uid, nil
	}

	u, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(u.Uid)
}

func lookupGID(name string) (int, error) {
	if gid, err := strconv.Atoi(name); err == nil {
		return gid, nil
	}

	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(g.Gid)
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
