package slurp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

func Execute(req Request) Response {
	if req.Src == "" {
		return Response{Failed: true, Msg: "src is required"}
	}

	info, err := os.Stat(req.Src)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Response{Failed: true, Msg: fmt.Sprintf("file not found: %s", req.Src)}
		}
		if errors.Is(err, fs.ErrPermission) {
			return Response{Failed: true, Msg: fmt.Sprintf("file is not readable: %s", req.Src)}
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			if pathErr.Err == syscall.ENOTDIR {
				return Response{Failed: true, Msg: fmt.Sprintf("unable to slurp file: %s: %v", req.Src, err)}
			}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("unable to slurp file: %s: %v", req.Src, err)}
	}

	if info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("source is a directory and must be a file: %s", req.Src)}
	}

	data, err := os.ReadFile(req.Src)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return Response{Failed: true, Msg: fmt.Sprintf("file is not readable: %s", req.Src)}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("unable to slurp file: %s: %v", req.Src, err)}
	}

	encoded := base64.StdEncoding.EncodeToString(data)

	return Response{
		Changed:  false,
		Content:  encoded,
		Source:   req.Src,
		Encoding: "base64",
	}
}
