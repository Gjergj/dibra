//go:build !windows

package template

import (
	"fmt"
	"os"
	"syscall"
)

func osStat(path string) (fileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileInfo{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileInfo{}, fmt.Errorf("failed to read file metadata")
	}
	return fileInfo{uid: int(stat.Uid)}, nil
}
