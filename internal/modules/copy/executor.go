package copy

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func Execute(req Request) Response {
	if req.Dest == "" {
		return Response{Failed: true, Msg: "dest is required"}
	}

	if req.Src == "" && req.Content == "" {
		return Response{Failed: true, Msg: "src or content is required"}
	}

	if req.Src != "" && req.Content != "" {
		return Response{Failed: true, Msg: "src and content are mutually exclusive"}
	}

	if req.Content != "" {
		return copyContent(req)
	}

	if req.RemoteSrc {
		return copyRemoteSrc(req)
	}

	return copyFromTemp(req)
}

func copyContent(req Request) Response {
	destInfo, err := os.Stat(req.Dest)
	destExists := err == nil

	if destExists && destInfo.IsDir() {
		return Response{Failed: true, Msg: "dest cannot be a directory when using content"}
	}

	newChecksum := sha1String([]byte(req.Content))

	if destExists && !req.Force {
		existingChecksum, _ := sha1File(req.Dest)
		if existingChecksum == newChecksum {
			return Response{Changed: false, Dest: req.Dest, Checksum: newChecksum, Msg: "content already matches"}
		}
	}

	var backupFile string
	if req.Backup && destExists {
		backupFile, err = createBackup(req.Dest)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	if err := os.MkdirAll(filepath.Dir(req.Dest), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	if err := atomicWrite(req.Dest, []byte(req.Content)); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write content: %v", err)}
	}

	if err := setAttributes(req.Dest, req.Owner, req.Group, req.Mode); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	info, _ := os.Stat(req.Dest)
	return Response{
		Changed:    true,
		Dest:       req.Dest,
		Checksum:   newChecksum,
		BackupFile: backupFile,
		Size:       info.Size(),
		Msg:        "content written",
	}
}

func copyRemoteSrc(req Request) Response {
	srcInfo, err := os.Stat(req.Src)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("src does not exist: %s", req.Src)}
	}

	if srcInfo.IsDir() {
		return copyDirectory(req)
	}

	return copySingleFile(req.Src, req.Dest, req)
}

func copyFromTemp(req Request) Response {
	srcInfo, err := os.Stat(req.Src)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("temp src does not exist: %s", req.Src)}
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

	if srcInfo.IsDir() {
		return copyDirectory(req)
	}

	return copySingleFile(req.Src, req.Dest, req)
}

func copySingleFile(src, dest string, req Request) Response {
	srcChecksum, err := sha1File(src)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to compute src checksum: %v", err)}
	}

	destInfo, err := os.Stat(dest)
	destExists := err == nil

	if destExists && destInfo.IsDir() {
		dest = filepath.Join(dest, filepath.Base(src))
		destInfo, err = os.Stat(dest)
		destExists = err == nil
	}

	if destExists && !req.Force {
		destChecksum, _ := sha1File(dest)
		if destChecksum == srcChecksum {
			return Response{Changed: false, Dest: dest, Checksum: srcChecksum, Msg: "file already matches"}
		}
	}

	var backupFile string
	if req.Backup && destExists {
		backupFile, err = createBackup(dest)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	if err := copyFile(src, dest); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to copy file: %v", err)}
	}

	if err := setAttributes(dest, req.Owner, req.Group, req.Mode); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	info, _ := os.Stat(dest)
	return Response{
		Changed:    true,
		Dest:       dest,
		Src:        src,
		Checksum:   srcChecksum,
		BackupFile: backupFile,
		Size:       info.Size(),
		Msg:        "file copied",
	}
}

func copyDirectory(req Request) Response {
	changed := false
	var lastDest string

	err := filepath.Walk(req.Src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(req.Src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(req.Dest, relPath)
		lastDest = destPath

		if info.IsDir() {
			if err := os.MkdirAll(destPath, info.Mode()); err != nil {
				return err
			}
			return nil
		}

		resp := copySingleFile(path, destPath, Request{
			Owner: req.Owner,
			Group: req.Group,
			Mode:  req.Mode,
			Force: req.Force,
		})
		if resp.Failed {
			return fmt.Errorf("%s", resp.Msg)
		}
		if resp.Changed {
			changed = true
		}

		return nil
	})

	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to copy directory: %v", err)}
	}

	return Response{Changed: changed, Dest: lastDest, Msg: "directory copied"}
}

func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	tmpDest := dest + ".tmp"
	destFile, err := os.Create(tmpDest)
	if err != nil {
		return err
	}

	_, err = io.Copy(destFile, srcFile)
	destFile.Close()
	if err != nil {
		os.Remove(tmpDest)
		return err
	}

	return os.Rename(tmpDest, dest)
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

	if err := copyFile(path, backupPath); err != nil {
		return "", err
	}

	return backupPath, nil
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
