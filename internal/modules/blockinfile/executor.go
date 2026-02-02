package blockinfile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultMarker      = "# {mark} ANSIBLE MANAGED BLOCK"
	defaultMarkerBegin = "BEGIN"
	defaultMarkerEnd   = "END"
)

func Execute(req Request) Response {
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required"}
	}

	if req.State == "" {
		req.State = "present"
	}

	if req.State != "present" && req.State != "absent" {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid state: %s (must be 'present' or 'absent')", req.State)}
	}

	if req.InsertAfter != "" && req.InsertBefore != "" {
		return Response{Failed: true, Msg: "insertafter and insertbefore are mutually exclusive"}
	}

	if req.Marker == "" {
		req.Marker = defaultMarker
	}
	if req.MarkerBegin == "" {
		req.MarkerBegin = defaultMarkerBegin
	}
	if req.MarkerEnd == "" {
		req.MarkerEnd = defaultMarkerEnd
	}

	info, err := os.Stat(req.Path)
	fileExists := err == nil

	if !fileExists && req.State == "absent" {
		return Response{Changed: false, Path: req.Path, Msg: fmt.Sprintf("file %s not present", req.Path)}
	}

	if !fileExists && !req.Create {
		return Response{Failed: true, Msg: fmt.Sprintf("path does not exist and create is not set: %s", req.Path)}
	}

	var lines []string
	var originalContent string
	lineSep := "\n"

	if fileExists {
		if info.IsDir() {
			return Response{Failed: true, Msg: fmt.Sprintf("path is a directory: %s", req.Path)}
		}

		content, err := os.ReadFile(req.Path)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to read file: %v", err)}
		}
		originalContent = string(content)

		if bytes.Contains(content, []byte("\r\n")) {
			lineSep = "\r\n"
		}

		if len(content) > 0 {
			lines = splitLinesKeepEndings(string(content))
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
		}
	}

	marker0 := strings.Replace(req.Marker, "{mark}", req.MarkerBegin, 1) + lineSep
	marker1 := strings.Replace(req.Marker, "{mark}", req.MarkerEnd, 1) + lineSep

	present := req.State == "present" && req.Block != ""

	var blockLines []string
	if present {
		block := req.Block
		if !strings.HasSuffix(block, "\n") && !strings.HasSuffix(block, "\r\n") {
			block = block + lineSep
		} else if lineSep == "\r\n" && strings.HasSuffix(block, "\n") && !strings.HasSuffix(block, "\r\n") {
			block = strings.TrimSuffix(block, "\n") + lineSep
		}

		blockLines = append(blockLines, marker0)
		for _, line := range splitLinesKeepEndings(block) {
			blockLines = append(blockLines, line)
		}
		blockLines = append(blockLines, marker1)
	}

	n0, n1 := findMarkers(lines, marker0, marker1)

	if n0 != -1 && n1 != -1 {
		if n0 < n1 {
			lines = append(lines[:n0], lines[n1+1:]...)
		} else {
			lines = append(lines[:n1], lines[n0+1:]...)
			n0 = n1
		}
	} else if n0 != -1 || n1 != -1 {
		if n0 != -1 {
			lines = append(lines[:n0], lines[n0+1:]...)
		} else {
			lines = append(lines[:n1], lines[n1+1:]...)
			n0 = n1
		}
	} else {
		n0 = calculateInsertPosition(lines, req.InsertAfter, req.InsertBefore, lineSep)
	}

	if n0 > 0 && len(lines) > 0 && n0 <= len(lines) {
		prevLine := lines[n0-1]
		if !strings.HasSuffix(prevLine, "\n") && !strings.HasSuffix(prevLine, "\r\n") {
			lines[n0-1] = prevLine + lineSep
		}
	}

	blankLine := lineSep
	if req.PrependNewline && present {
		if n0 != 0 && len(lines) > 0 {
			prevIdx := n0 - 1
			if prevIdx >= 0 && prevIdx < len(lines) {
				prevLine := strings.TrimSuffix(strings.TrimSuffix(lines[prevIdx], "\n"), "\r")
				if prevLine != "" {
					lines = insertAt(lines, n0, blankLine)
					n0++
				}
			}
		}
	}

	lines = insertBlockAt(lines, n0, blockLines)

	if req.AppendNewline && present {
		lineAfterBlock := n0 + len(blockLines)
		if lineAfterBlock < len(lines) {
			nextLine := strings.TrimSuffix(strings.TrimSuffix(lines[lineAfterBlock], "\n"), "\r")
			if nextLine != "" {
				lines = insertAt(lines, lineAfterBlock, blankLine)
			}
		}
	}

	newContent := strings.Join(lines, "")

	changed := originalContent != newContent

	if !changed {
		attrChanged, err := setAttributesIfDifferent(req.Path, req.Owner, req.Group, req.Mode)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
		}
		msg := "Block already present"
		if req.State == "absent" {
			msg = "Block already absent"
		}
		return Response{
			Changed: attrChanged,
			Path:    req.Path,
			Msg:     msg,
		}
	}

	if req.Validate != "" {
		if err := validateContent(req.Validate, []byte(newContent)); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("validation failed: %v", err)}
		}
	}

	var backupFile string
	if req.Backup && fileExists {
		backupFile, err = createBackup(req.Path)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	if err := atomicWrite(req.Path, []byte(newContent)); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write file: %v", err)}
	}

	if _, err := setAttributesIfDifferent(req.Path, req.Owner, req.Group, req.Mode); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	var msg string
	if !fileExists {
		msg = "File created"
	} else if present {
		msg = "Block inserted"
	} else {
		msg = "Block removed"
	}

	return Response{
		Changed:    true,
		Path:       req.Path,
		Msg:        msg,
		BackupFile: backupFile,
		Diff: &Diff{
			Before: originalContent,
			After:  newContent,
		},
	}
}

func splitLinesKeepEndings(content string) []string {
	if content == "" {
		return []string{}
	}

	var lines []string
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines = append(lines, content[start:i+1])
			start = i + 1
		} else if content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n' {
			lines = append(lines, content[start:i+2])
			start = i + 2
			i++
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}

	return lines
}

func findMarkers(lines []string, marker0, marker1 string) (n0, n1 int) {
	n0, n1 = -1, -1
	marker0Plain := strings.TrimSuffix(strings.TrimSuffix(marker0, "\n"), "\r")
	marker1Plain := strings.TrimSuffix(strings.TrimSuffix(marker1, "\n"), "\r")

	for i, line := range lines {
		linePlain := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if linePlain == marker0Plain {
			n0 = i
		}
		if linePlain == marker1Plain {
			n1 = i
		}
	}
	return n0, n1
}

func calculateInsertPosition(lines []string, insertAfter, insertBefore, lineSep string) int {
	if insertBefore == "BOF" {
		return 0
	}

	if insertAfter == "" && insertBefore == "" {
		insertAfter = "EOF"
	}

	if insertAfter == "EOF" {
		return len(lines)
	}

	var insertRe *regexp.Regexp
	var err error
	var searchPattern string
	var isAfter bool

	if insertAfter != "" && insertAfter != "EOF" {
		searchPattern = insertAfter
		isAfter = true
	} else if insertBefore != "" && insertBefore != "BOF" {
		searchPattern = insertBefore
		isAfter = false
	}

	if searchPattern == "" {
		return len(lines)
	}

	isMultiline := strings.Contains(searchPattern, "(?m)")

	if isMultiline {
		insertRe, err = regexp.Compile(searchPattern)
		if err != nil {
			return len(lines)
		}

		fullContent := strings.Join(lines, "")
		match := insertRe.FindStringIndex(fullContent)
		if match != nil {
			var targetPos int
			if isAfter {
				targetPos = match[1]
			} else {
				targetPos = match[0]
			}

			pos := 0
			for i, line := range lines {
				pos += len(line)
				if pos >= targetPos {
					if isAfter {
						return i + 1
					}
					return i
				}
			}
		}
		return len(lines)
	}

	insertRe, err = regexp.Compile(searchPattern)
	if err != nil {
		return len(lines)
	}

	lastMatch := -1
	for i, line := range lines {
		linePlain := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if insertRe.MatchString(linePlain) {
			lastMatch = i
		}
	}

	if lastMatch == -1 {
		return len(lines)
	}

	if isAfter {
		return lastMatch + 1
	}
	return lastMatch
}

func insertAt(lines []string, index int, line string) []string {
	if index >= len(lines) {
		return append(lines, line)
	}
	if index <= 0 {
		return append([]string{line}, lines...)
	}

	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:index]...)
	result = append(result, line)
	result = append(result, lines[index:]...)
	return result
}

func insertBlockAt(lines []string, index int, blockLines []string) []string {
	if len(blockLines) == 0 {
		return lines
	}

	if index >= len(lines) {
		return append(lines, blockLines...)
	}
	if index <= 0 {
		return append(blockLines, lines...)
	}

	result := make([]string, 0, len(lines)+len(blockLines))
	result = append(result, lines[:index]...)
	result = append(result, blockLines...)
	result = append(result, lines[index:]...)
	return result
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
	tmpFile, err := os.CreateTemp("", "blockinfile-validate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	cmdStr := strings.Replace(validateCmd, "%s", tmpFile.Name(), -1)
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validation command failed: %s - %s", err, string(output))
	}

	return nil
}

func setAttributesIfDifferent(path, owner, group, mode string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}

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

		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false, fmt.Errorf("failed to get file stat")
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

		info, err := os.Lstat(path)
		if err != nil {
			return false, err
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
