//go:build windows

package template

import "os"

func osStat(path string) (fileInfo, error) {
	_, err := os.Stat(path)
	if err != nil {
		return fileInfo{}, err
	}
	return fileInfo{uid: 0}, nil
}
