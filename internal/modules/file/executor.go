package file

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

func Execute(req Request) Response {
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	if req.State == "" {
		req.State = detectState(req.Path)
	}

	switch req.State {
	case "absent":
		return ensureAbsent(req)
	case "directory":
		return ensureDirectory(req)
	case "file":
		return ensureFile(req)
	case "link":
		return ensureSymlink(req)
	case "hard":
		return ensureHardlink(req)
	case "touch":
		return ensureTouch(req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

func detectState(path string) string {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		return "file"
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return "link"
	}
	if info.IsDir() {
		return "directory"
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok && stat.Nlink > 1 {
		return "hard"
	}

	return "file"
}

func ensureAbsent(req Request) Response {
	info, err := os.Lstat(req.Path)
	if os.IsNotExist(err) {
		return Response{Changed: false, Path: req.Path, State: "absent", Msg: "path already absent"}
	}
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat path: %v", err)}
	}

	if info.IsDir() {
		if err := os.RemoveAll(req.Path); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove directory: %v", err)}
		}
	} else {
		if err := os.Remove(req.Path); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove file: %v", err)}
		}
	}

	return Response{Changed: true, Path: req.Path, State: "absent", Msg: "path removed"}
}

func ensureDirectory(req Request) Response {
	info, err := os.Lstat(req.Path)
	exists := err == nil

	if exists && !info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("path exists but is not a directory: %s", req.Path)}
	}

	changed := false

	if !exists {
		mode := os.FileMode(0755)
		if req.Mode != "" {
			if m, err := parseMode(req.Mode); err == nil {
				mode = m
			}
		}

		if err := os.MkdirAll(req.Path, mode); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create directory: %v", err)}
		}
		changed = true
	}

	attrChanged, err := setAttributes(req.Path, req.Owner, req.Group, req.Mode)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}
	changed = changed || attrChanged

	if req.Recurse {
		err := filepath.Walk(req.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			attrChanged, err := setAttributes(path, req.Owner, req.Group, req.Mode)
			if err != nil {
				return err
			}
			if attrChanged {
				changed = true
			}
			return nil
		})
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set recursive attributes: %v", err)}
		}
	}

	return Response{Changed: changed, Path: req.Path, State: "directory"}
}

func ensureFile(req Request) Response {
	info, err := os.Lstat(req.Path)
	if os.IsNotExist(err) {
		return Response{Failed: true, Msg: fmt.Sprintf("path does not exist: %s (use state=touch to create)", req.Path)}
	}
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat path: %v", err)}
	}

	if info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("path is a directory, not a file: %s", req.Path)}
	}

	changed, err := setAttributes(req.Path, req.Owner, req.Group, req.Mode)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	return Response{Changed: changed, Path: req.Path, State: "file"}
}

func ensureSymlink(req Request) Response {
	if req.Src == "" {
		return Response{Failed: true, Msg: "src is required for state=link"}
	}

	info, err := os.Lstat(req.Path)
	exists := err == nil

	if exists {
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(req.Path)
			if err == nil && target == req.Src {
				return Response{Changed: false, Dest: req.Path, State: "link", Msg: "symlink already correct"}
			}
		}

		if !req.Force {
			return Response{Failed: true, Msg: fmt.Sprintf("path exists and is not a symlink to %s (use force=true)", req.Src)}
		}

		if err := os.RemoveAll(req.Path); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove existing path: %v", err)}
		}
	}

	if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	if err := os.Symlink(req.Src, req.Path); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create symlink: %v", err)}
	}

	return Response{Changed: true, Dest: req.Path, State: "link", Msg: fmt.Sprintf("symlink created: %s -> %s", req.Path, req.Src)}
}

func ensureHardlink(req Request) Response {
	if req.Src == "" {
		return Response{Failed: true, Msg: "src is required for state=hard"}
	}

	srcInfo, err := os.Stat(req.Src)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("src does not exist: %s", req.Src)}
	}

	srcStat, ok := srcInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return Response{Failed: true, Msg: "failed to get src inode"}
	}

	info, err := os.Lstat(req.Path)
	exists := err == nil

	if exists {
		pathStat, ok := info.Sys().(*syscall.Stat_t)
		if ok && pathStat.Ino == srcStat.Ino {
			return Response{Changed: false, Dest: req.Path, State: "hard", Msg: "hardlink already correct"}
		}

		if !req.Force {
			return Response{Failed: true, Msg: fmt.Sprintf("path exists and is not a hardlink to %s (use force=true)", req.Src)}
		}

		if err := os.Remove(req.Path); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to remove existing path: %v", err)}
		}
	}

	if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
	}

	if err := os.Link(req.Src, req.Path); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create hardlink: %v", err)}
	}

	return Response{Changed: true, Dest: req.Path, State: "hard", Msg: fmt.Sprintf("hardlink created: %s -> %s", req.Path, req.Src)}
}

func ensureTouch(req Request) Response {
	info, err := os.Lstat(req.Path)
	exists := err == nil

	changed := false

	if !exists {
		if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
		}

		f, err := os.Create(req.Path)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create file: %v", err)}
		}
		f.Close()
		changed = true
	} else {
		now := syscall.NsecToTimeval(0)
		if err := syscall.Utimes(req.Path, []syscall.Timeval{now, now}); err != nil {
			if err := os.Chtimes(req.Path, info.ModTime(), info.ModTime()); err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("failed to update timestamps: %v", err)}
			}
		}
		changed = true
	}

	attrChanged, err := setAttributes(req.Path, req.Owner, req.Group, req.Mode)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}
	changed = changed || attrChanged

	return Response{Changed: changed, Dest: req.Path, State: "touch"}
}

func setAttributes(path, owner, group, mode string) (bool, error) {
	changed := false

	if owner != "" {
		uid, err := lookupUID(owner)
		if err != nil {
			return false, fmt.Errorf("failed to lookup owner %s: %w", owner, err)
		}

		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false, fmt.Errorf("failed to get file stat")
		}

		if int(stat.Uid) != uid {
			if err := os.Lchown(path, uid, -1); err != nil {
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

		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false, fmt.Errorf("failed to get file stat")
		}

		if int(stat.Gid) != gid {
			if err := os.Lchown(path, -1, gid); err != nil {
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

		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return changed, nil
		}

		currentMode := info.Mode().Perm()
		if currentMode != newMode {
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
