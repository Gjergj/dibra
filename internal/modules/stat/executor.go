package stat

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
)

func Execute(req Request) Response {
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	var info os.FileInfo
	var err error

	if req.Follow {
		info, err = os.Stat(req.Path)
	} else {
		info, err = os.Lstat(req.Path)
	}

	if os.IsNotExist(err) {
		return Response{
			Changed: false,
			Exists:  false,
			Stat:    &Stat{Exists: false, Path: req.Path},
		}
	}

	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat: %v", err)}
	}

	stat := &Stat{
		Exists: true,
		IsDir:  info.IsDir(),
		IsReg:  info.Mode().IsRegular(),
		IsLnk:  info.Mode()&os.ModeSymlink != 0,
		Path:   req.Path,
		Mode:   fmt.Sprintf("%04o", info.Mode().Perm()),
		Size:   info.Size(),
	}

	if sysStat, ok := info.Sys().(*syscall.Stat_t); ok {
		stat.UID = int(sysStat.Uid)
		stat.GID = int(sysStat.Gid)
	}

	if stat.IsReg {
		checksum, err := sha1File(req.Path)
		if err == nil {
			stat.Checksum = checksum
		}
	}

	return Response{
		Changed: false,
		Exists:  true,
		Stat:    stat,
	}
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
