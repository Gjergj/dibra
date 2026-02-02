package tempfile

import (
	"fmt"
	"os"
)

const defaultPrefix = "ansible."

func Execute(req Request) Response {
	prefix := defaultPrefix
	if req.Prefix != nil {
		prefix = *req.Prefix
	}

	suffix := req.Suffix

	state := req.State
	if state == "" {
		state = "file"
	}

	if state != "file" && state != "directory" {
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("state must be 'file' or 'directory', got: %s", state),
		}
	}

	dir := req.Path
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return Response{
					Failed: true,
					Msg:    fmt.Sprintf("path does not exist: %s", dir),
				}
			}
			return Response{
				Failed: true,
				Msg:    fmt.Sprintf("failed to stat path: %v", err),
			}
		}
		if !info.IsDir() {
			return Response{
				Failed: true,
				Msg:    fmt.Sprintf("path is not a directory: %s", dir),
			}
		}
	}

	var path string
	var err error

	if state == "file" {
		var f *os.File
		f, err = os.CreateTemp(dir, prefix+"*"+suffix)
		if err == nil {
			path = f.Name()
			f.Close()
		}
	} else {
		path, err = os.MkdirTemp(dir, prefix+"*"+suffix)
	}

	if err != nil {
		return Response{
			Failed: true,
			Msg:    fmt.Sprintf("failed to create temp %s: %v", state, err),
		}
	}

	return Response{
		Changed: true,
		Path:    path,
		State:   state,
	}
}
