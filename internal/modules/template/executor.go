package template

import (
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
	"time"
)

func Execute(req Request) Response {
	if req.Dest == "" {
		return Response{Failed: true, Msg: "dest is required"}
	}

	if req.Content == "" {
		return Response{Failed: true, Msg: "content is required"}
	}

	destPath := resolveDestPath(req)
	if destPath == "" {
		return Response{Failed: true, Msg: "unable to resolve dest path"}
	}

	if req.Follow {
		if info, err := os.Lstat(destPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(destPath)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to resolve symlink %s: %v", destPath, err)}
			}
			destPath = resolved
		}
	} else {
		if info, err := os.Lstat(destPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return Response{Failed: true, Msg: "dest is a symlink (follow=false)"}
		}
	}

	if req.Follow {
		if parentInfo, err := os.Lstat(filepath.Dir(destPath)); err == nil && parentInfo.Mode()&os.ModeSymlink != 0 {
			resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(destPath))
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to resolve dest parent symlink: %v", err)}
			}
			destPath = filepath.Join(resolvedParent, filepath.Base(destPath))
		}
	} else {
		if parentInfo, err := os.Lstat(filepath.Dir(destPath)); err == nil && parentInfo.Mode()&os.ModeSymlink != 0 {
			return Response{Failed: true, Msg: "dest parent is a symlink (follow=false)"}
		}
	}

	destInfo, err := os.Stat(destPath)
	destExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat dest: %v", err)}
	}

	if destExists && destInfo.IsDir() {
		return Response{Failed: true, Msg: "dest cannot be a directory"}
	}

	newChecksum := sha1String([]byte(req.Content))
	if destExists {
		existingChecksum, _ := sha1File(destPath)
		if existingChecksum == newChecksum {
			attrChanged, err := setAttributesIfDifferent(destPath, req.Owner, req.Group, req.Mode)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
			}
			return responseFromStat(destPath, req, newChecksum, attrChanged, "content already matches", "")
		}
		if !req.Force {
			return responseFromStat(destPath, req, newChecksum, false, "dest exists and force=false", "")
		}
	}

	if req.Validate != "" {
		if err := validateContent(req.Validate, []byte(req.Content)); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("validation failed: %v", err)}
		}
	}

	var backupFile string
	if req.Backup && destExists {
		backupFile, err = createBackup(destPath)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	if err := atomicWrite(destPath, []byte(req.Content)); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write content: %v", err)}
	}

	if _, err := setAttributesIfDifferent(destPath, req.Owner, req.Group, req.Mode); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	return responseFromStat(destPath, req, newChecksum, true, "content written", backupFile)
}

func responseFromStat(path string, req Request, checksum string, changed bool, msg string, backupFile string) Response {
	info, err := os.Stat(path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat file: %v", err)}
	}

	return Response{
		Changed:    changed,
		Dest:       req.Dest,
		Src:        req.Src,
		Checksum:   checksum,
		BackupFile: backupFile,
		Size:       info.Size(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
		Owner:      lookupUserName(info),
		Group:      lookupGroupName(info),
		Msg:        msg,
	}
}

func resolveDestPath(req Request) string {
	if req.Src == "" {
		return req.Dest
	}

	if strings.HasSuffix(req.Dest, "/") {
		trimmed := strings.TrimRight(req.Dest, "/")
		if trimmed == "" {
			return "/" + filepath.Base(req.Src)
		}
		return filepath.Join(trimmed, filepath.Base(req.Src))
	}

	info, err := os.Lstat(req.Dest)
	if err == nil && info.IsDir() {
		return filepath.Join(req.Dest, filepath.Base(req.Src))
	}

	return req.Dest
}

func atomicWrite(dest string, data []byte) error {
	tmpDest := dest + ".tmp"
	if err := os.WriteFile(tmpDest, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpDest, dest)
}

func createBackup(path string) (string, error) {
	timestamp := time.Now().Format("2006-01-02@15:04:05")
	backupPath := fmt.Sprintf("%s.%s~", path, timestamp)

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", err
	}

	return backupPath, nil
}

func validateContent(validateCmd string, content []byte) error {
	tmpFile, err := os.CreateTemp("", "template-validate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	cmdStr := strings.ReplaceAll(validateCmd, "%s", tmpFile.Name())
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validation command failed: %s - %s", err, string(output))
	}

	return nil
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

func sha1String(data []byte) string {
	h := sha1.Sum(data)
	return hex.EncodeToString(h[:])
}

func setAttributesIfDifferent(path, owner, group, mode string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("failed to get file stat")
	}

	changed := false

	if owner != "" {
		uid, err := lookupUID(owner)
		if err != nil {
			return false, fmt.Errorf("failed to lookup owner %s: %w", owner, err)
		}
		if int(stat.Uid) != uid {
			if err := os.Chown(path, uid, -1); err != nil {
				return false, fmt.Errorf("failed to chown: %w", err)
			}
			changed = true
		}
	}

	if group != "" {
		gid, err := lookupGID(group)
		if err != nil {
			return false, fmt.Errorf("failed to lookup group %s: %w", group, err)
		}
		if int(stat.Gid) != gid {
			if err := os.Chown(path, -1, gid); err != nil {
				return false, fmt.Errorf("failed to chgrp: %w", err)
			}
			changed = true
		}
	}

	if mode != "" {
		newMode, err := parseMode(mode)
		if err != nil {
			return false, fmt.Errorf("invalid mode %s: %w", mode, err)
		}
		if info.Mode().Perm() != newMode.Perm() {
			if err := os.Chmod(path, newMode); err != nil {
				return false, fmt.Errorf("failed to chmod: %w", err)
			}
			changed = true
		}
	}

	return changed, nil
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

func lookupUserName(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	userInfo, err := user.LookupId(strconv.Itoa(int(stat.Uid)))
	if err != nil {
		return ""
	}
	return userInfo.Username
}

func lookupGroupName(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	groupInfo, err := user.LookupGroupId(strconv.Itoa(int(stat.Gid)))
	if err != nil {
		return ""
	}
	return groupInfo.Name
}
