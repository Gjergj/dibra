package find

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func Execute(req Request) Response {
	if len(req.Paths) == 0 {
		return Response{Failed: true, Msg: "paths is required"}
	}

	if req.FileType == "" {
		req.FileType = "file"
	}
	if req.AgeStamp == "" {
		req.AgeStamp = "mtime"
	}
	if req.ChecksumAlgorithm == "" {
		req.ChecksumAlgorithm = "sha1"
	}

	if len(req.Patterns) == 0 {
		if req.UseRegex {
			req.Patterns = []string{".*"}
		} else {
			req.Patterns = []string{"*"}
		}
	}

	age, err := parseAge(req.Age)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to process age: %s", req.Age)}
	}

	size, err := parseSize(req.Size)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to process size: %s", req.Size)}
	}

	if req.Limit < 0 {
		return Response{Failed: true, Msg: fmt.Sprintf("limit cannot be %d (use 0 for unlimited)", req.Limit)}
	}

	var modeVal int64
	var hasMode bool
	if req.Mode != "" {
		parsed, err := strconv.ParseInt(req.Mode, 8, 32)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to parse mode: %s", req.Mode)}
		}
		modeVal = parsed
		hasMode = true
	}

	now := float64(time.Now().Unix())
	filelist := make([]FileInfo, 0)
	skipped := make(map[string]string)
	examined := 0
	hasWarnings := false
	msg := "All paths examined"
	limitReached := false

	for _, npath := range req.Paths {
		if limitReached {
			break
		}

		info, err := os.Stat(npath)
		if err != nil || !info.IsDir() {
			errMsg := fmt.Sprintf("'%s' is not a directory", npath)
			if err != nil {
				errMsg = err.Error()
			}
			skipped[npath] = errMsg
			hasWarnings = true
			continue
		}

		if !req.Recurse {
			entries, err := os.ReadDir(npath)
			if err != nil {
				skipped[npath] = err.Error()
				hasWarnings = true
				continue
			}

			for _, entry := range entries {
				examined++
				if strings.HasPrefix(entry.Name(), ".") && !req.Hidden {
					continue
				}
				fsname := filepath.Join(npath, entry.Name())
				matched, fi := processEntry(fsname, entry.Name(), req, age, size, now, hasMode, modeVal, skipped, &hasWarnings)
				if matched {
					filelist = append(filelist, fi)
					if req.Limit > 0 && len(filelist) >= req.Limit {
						limitReached = true
						msg = "Limit of matches reached"
						break
					}
				}
			}
		} else {
			walkFunc := func(fsname string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					if os.IsPermission(walkErr) {
						skipped[fsname] = walkErr.Error()
						hasWarnings = true
						return fs.SkipDir
					}
					return walkErr
				}

				if fsname == npath {
					return nil
				}

				examined++

				if req.Depth > 0 {
					wpath := strings.TrimRight(npath, string(os.PathSeparator)) + string(os.PathSeparator)
					depth := strings.Count(fsname, string(os.PathSeparator)) - strings.Count(wpath, string(os.PathSeparator)) + 1
					if depth > req.Depth {
						if d.IsDir() {
							return fs.SkipDir
						}
						return nil
					}
				}

				basename := filepath.Base(fsname)
				if strings.HasPrefix(basename, ".") && !req.Hidden {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}

				matched, fi := processEntry(fsname, basename, req, age, size, now, hasMode, modeVal, skipped, &hasWarnings)
				if matched {
					filelist = append(filelist, fi)
					if req.Limit > 0 && len(filelist) >= req.Limit {
						return fmt.Errorf("limit reached")
					}
				}
				return nil
			}

			if req.Follow {
				err = walkFollowSymlinks(npath, walkFunc)
			} else {
				err = filepath.WalkDir(npath, walkFunc)
			}
			if err != nil && err.Error() == "limit reached" {
				limitReached = true
				msg = "Limit of matches reached"
			} else if err != nil {
				skipped[npath] = err.Error()
				hasWarnings = true
			}
		}
	}

	if hasWarnings && msg == "All paths examined" {
		msg = "Not all paths examined, check warnings for details"
	}

	return Response{
		Changed:      false,
		Msg:          msg,
		Files:        filelist,
		Matched:      len(filelist),
		Examined:     examined,
		SkippedPaths: skipped,
	}
}

func processEntry(fsname, basename string, req Request, age *int64, size *int64, now float64, hasMode bool, modeVal int64, skipped map[string]string, hasWarnings *bool) (bool, FileInfo) {
	var st syscall.Stat_t
	var err error

	if req.Follow {
		err = syscall.Stat(fsname, &st)
	} else {
		err = syscall.Lstat(fsname, &st)
	}
	if err != nil {
		skipped[fsname] = err.Error()
		*hasWarnings = true
		return false, FileInfo{}
	}

	ftype := st.Mode & syscall.S_IFMT
	isDir := ftype == syscall.S_IFDIR
	isReg := ftype == syscall.S_IFREG
	isLnk := ftype == syscall.S_IFLNK

	switch req.FileType {
	case "file":
		if !isReg {
			return false, FileInfo{}
		}
	case "directory":
		if !isDir {
			return false, FileInfo{}
		}
	case "link":
		if !isLnk {
			return false, FileInfo{}
		}
	case "any":
	}

	if !pfilter(basename, req.Patterns, req.Excludes, req.UseRegex) {
		return false, FileInfo{}
	}

	if !ageFilterEntry(&st, now, age, req.AgeStamp) {
		return false, FileInfo{}
	}

	if hasMode && !modeFilterEntry(&st, modeVal, req.ExactMode) {
		return false, FileInfo{}
	}

	if req.FileType == "any" {
		if isReg && !sizeFilterEntry(&st, size) {
			return false, FileInfo{}
		}
	} else if isReg {
		if !sizeFilterEntry(&st, size) {
			return false, FileInfo{}
		}
	}

	if isReg && req.Contains != "" {
		match, err := contentFilter(fsname, req.Contains, req.ReadWholeFile)
		if err != nil {
			skipped[fsname] = err.Error()
			*hasWarnings = true
			return false, FileInfo{}
		}
		if !match {
			return false, FileInfo{}
		}
	}

	fi := buildFileInfo(fsname, &st)

	if isReg && req.GetChecksum {
		checksum, err := fileChecksum(fsname, req.ChecksumAlgorithm)
		if err == nil {
			fi.Checksum = checksum
		}
	}

	return true, fi
}

func walkFollowSymlinks(root string, fn fs.WalkDirFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fn(path, nil, err)
		}
		return fn(path, fs.FileInfoToDirEntry(info), nil)
	})
}

func pfilter(basename string, patterns, excludes []string, useRegex bool) bool {
	if len(patterns) == 0 && len(excludes) == 0 {
		return true
	}

	matched := false
	if useRegex {
		for _, p := range patterns {
			r, err := regexp.Compile(p)
			if err != nil {
				continue
			}
			if r.MatchString(basename) {
				matched = true
				break
			}
		}
	} else {
		for _, p := range patterns {
			if m, _ := filepath.Match(p, basename); m {
				matched = true
				break
			}
		}
	}

	if !matched {
		return false
	}

	if len(excludes) > 0 {
		if useRegex {
			for _, e := range excludes {
				r, err := regexp.Compile(e)
				if err != nil {
					continue
				}
				if r.MatchString(basename) {
					return false
				}
			}
		} else {
			for _, e := range excludes {
				if m, _ := filepath.Match(e, basename); m {
					return false
				}
			}
		}
	}

	return true
}

func contentFilter(path, pattern string, readWholeFile bool) (bool, error) {
	prog, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid contains regex: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	if readWholeFile {
		data, err := io.ReadAll(f)
		if err != nil {
			return false, err
		}
		return prog.Match(data), nil
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if prog.MatchString(line) {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func parseAge(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	s = strings.ToLower(s)
	r := regexp.MustCompile(`^(-?\d+)(s|m|h|d|w)?$`)
	m := r.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("invalid age: %s", s)
	}
	val, _ := strconv.ParseInt(m[1], 10, 64)
	multipliers := map[string]int64{"s": 1, "m": 60, "h": 3600, "d": 86400, "w": 604800}
	mult := int64(1)
	if m[2] != "" {
		mult = multipliers[m[2]]
	}
	result := val * mult
	return &result, nil
}

func parseSize(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	s = strings.ToLower(s)
	r := regexp.MustCompile(`^(-?\d+)(b|k|m|g|t)?$`)
	m := r.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("invalid size: %s", s)
	}
	val, _ := strconv.ParseInt(m[1], 10, 64)
	multipliers := map[string]int64{"b": 1, "k": 1024, "m": 1024 * 1024, "g": 1024 * 1024 * 1024, "t": 1024 * 1024 * 1024 * 1024}
	mult := int64(1)
	if m[2] != "" {
		mult = multipliers[m[2]]
	}
	result := val * mult
	return &result, nil
}

func fileChecksum(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var h hash.Hash
	switch algo {
	case "md5":
		h = md5.New()
	case "sha256":
		h = sha256.New()
	case "sha384":
		h = sha512.New384()
	case "sha512":
		h = sha512.New()
	default:
		h = sha1.New()
	}

	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sizeFilterEntry(st *syscall.Stat_t, size *int64) bool {
	if size == nil {
		return true
	}
	if *size >= 0 {
		return st.Size >= int64(math.Abs(float64(*size)))
	}
	return st.Size <= int64(math.Abs(float64(*size)))
}

func modeFilterEntry(st *syscall.Stat_t, mode int64, exact bool) bool {
	stMode := int64(st.Mode) & 0777
	if exact {
		return stMode == mode
	}
	return stMode&mode == mode
}

func buildFileInfo(path string, st *syscall.Stat_t) FileInfo {
	perm := os.FileMode(st.Mode).Perm()

	pwName := ""
	if u, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil {
		pwName = u.Username
	}

	grName := ""
	if g, err := user.LookupGroupId(strconv.Itoa(int(st.Gid))); err == nil {
		grName = g.Name
	}

	atime, mtime, ctime := getStatTimes(st)

	return FileInfo{
		Path:   path,
		Mode:   fmt.Sprintf("%04o", perm),
		IsDir:  st.Mode&syscall.S_IFMT == syscall.S_IFDIR,
		IsChr:  st.Mode&syscall.S_IFMT == syscall.S_IFCHR,
		IsBlk:  st.Mode&syscall.S_IFMT == syscall.S_IFBLK,
		IsReg:  st.Mode&syscall.S_IFMT == syscall.S_IFREG,
		IsFIFO: st.Mode&syscall.S_IFMT == syscall.S_IFIFO,
		IsLnk:  st.Mode&syscall.S_IFMT == syscall.S_IFLNK,
		IsSock: st.Mode&syscall.S_IFMT == syscall.S_IFSOCK,
		UID:    int(st.Uid),
		GID:    int(st.Gid),
		Size:   st.Size,
		Inode:  st.Ino,
		Dev:    uint64(st.Dev),
		Nlink:  uint64(st.Nlink),
		Atime:  atime,
		Mtime:  mtime,
		Ctime:  ctime,
		GrName: grName,
		PwName: pwName,
		Wusr:   st.Mode&syscall.S_IWUSR != 0,
		Rusr:   st.Mode&syscall.S_IRUSR != 0,
		Xusr:   st.Mode&syscall.S_IXUSR != 0,
		Wgrp:   st.Mode&syscall.S_IWGRP != 0,
		Rgrp:   st.Mode&syscall.S_IRGRP != 0,
		Xgrp:   st.Mode&syscall.S_IXGRP != 0,
		Woth:   st.Mode&syscall.S_IWOTH != 0,
		Roth:   st.Mode&syscall.S_IROTH != 0,
		Xoth:   st.Mode&syscall.S_IXOTH != 0,
		IsUID:  st.Mode&syscall.S_ISUID != 0,
		IsGID:  st.Mode&syscall.S_ISGID != 0,
	}
}
