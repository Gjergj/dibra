package lineinfile

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

	if req.Backrefs && req.SearchString != "" {
		return Response{Failed: true, Msg: "backrefs and search_string are mutually exclusive"}
	}

	if req.Regexp != "" && req.SearchString != "" {
		return Response{Failed: true, Msg: "regexp and search_string are mutually exclusive"}
	}

	if req.InsertAfter != "" && req.InsertBefore != "" {
		return Response{Failed: true, Msg: "insertafter and insertbefore are mutually exclusive"}
	}

	if req.Backrefs && (req.InsertAfter != "" || req.InsertBefore != "") {
		return Response{Failed: true, Msg: "backrefs cannot be used with insertafter or insertbefore"}
	}

	if req.State == "present" {
		if req.Line == "" {
			return Response{Failed: true, Msg: "line is required for state=present"}
		}
		if req.Backrefs && req.Regexp == "" {
			return Response{Failed: true, Msg: "regexp is required when using backrefs"}
		}
		return present(req)
	}

	if req.Regexp == "" && req.SearchString == "" && req.Line == "" {
		return Response{Failed: true, Msg: "one of regexp, search_string, or line is required for state=absent"}
	}
	return absent(req)
}

func present(req Request) Response {
	info, err := os.Stat(req.Path)
	fileExists := err == nil

	if !fileExists && !req.Create {
		return Response{Failed: true, Msg: fmt.Sprintf("path does not exist and create is not set: %s", req.Path)}
	}

	var lines []string
	var originalContent string
	var lineSep string = "\n"

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
			lines = splitLines(string(content))
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(req.Path), 0755); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create parent directory: %v", err)}
		}
	}

	var re *regexp.Regexp
	if req.Regexp != "" {
		var err error
		re, err = regexp.Compile(req.Regexp)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid regexp: %v", err)}
		}
	}

	var insertRe *regexp.Regexp
	insertAfter := req.InsertAfter
	insertBefore := req.InsertBefore

	if insertAfter != "" && insertAfter != "EOF" && insertAfter != "BOF" {
		var err error
		insertRe, err = regexp.Compile(insertAfter)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid insertafter regexp: %v", err)}
		}
	}

	if insertBefore != "" && insertBefore != "EOF" && insertBefore != "BOF" {
		var err error
		insertRe, err = regexp.Compile(insertBefore)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid insertbefore regexp: %v", err)}
		}
	}

	if insertAfter == "" && insertBefore == "" {
		insertAfter = "EOF"
	}

	matchIndex := -1
	insertIndex := -1
	var matchObj []string

	for i := 0; i < len(lines); i++ {
		idx := i
		if !req.FirstMatch {
			idx = len(lines) - 1 - i
		}
		line := lines[idx]

		if re != nil {
			if match := re.FindStringSubmatch(line); match != nil {
				matchIndex = idx
				matchObj = match
				break
			}
		} else if req.SearchString != "" {
			if strings.Contains(line, req.SearchString) {
				matchIndex = idx
				break
			}
		}
	}

	if matchIndex == -1 && insertRe != nil {
		for i := 0; i < len(lines); i++ {
			idx := i
			if !req.FirstMatch {
				idx = len(lines) - 1 - i
			}
			if insertRe.MatchString(lines[idx]) {
				insertIndex = idx
				break
			}
		}
	}

	newLine := req.Line
	if !strings.HasSuffix(newLine, "\n") && !strings.HasSuffix(newLine, "\r\n") {
		newLine = newLine + lineSep
	} else if lineSep == "\r\n" && strings.HasSuffix(newLine, "\n") && !strings.HasSuffix(newLine, "\r\n") {
		newLine = strings.TrimSuffix(newLine, "\n") + lineSep
	}

	changed := false
	msg := ""

	if matchIndex != -1 {
		if req.Backrefs && re != nil && len(matchObj) > 0 {
			newLine = expandBackrefs(req.Line, matchObj)
			if !strings.HasSuffix(newLine, lineSep) {
				newLine = newLine + lineSep
			}
		}

		if lines[matchIndex] != strings.TrimSuffix(newLine, lineSep) {
			lines[matchIndex] = strings.TrimSuffix(newLine, lineSep)
			changed = true
			msg = "line replaced"
		}
	} else if req.Backrefs {
		msg = "skipped, no match for backrefs"
	} else {
		lineContent := strings.TrimSuffix(newLine, lineSep)

		alreadyPresent := false
		for _, l := range lines {
			if l == lineContent {
				alreadyPresent = true
				break
			}
		}

		if alreadyPresent {
			msg = "line already present"
		} else {
			changed = true
			msg = "line added"

			if insertBefore == "BOF" {
				lines = append([]string{lineContent}, lines...)
			} else if insertAfter == "EOF" || insertIndex == -1 {
				lines = append(lines, lineContent)
			} else if insertBefore != "" && insertIndex != -1 {
				lines = insertAt(lines, insertIndex, lineContent)
			} else if insertAfter != "" && insertIndex != -1 {
				lines = insertAt(lines, insertIndex+1, lineContent)
			} else {
				lines = append(lines, lineContent)
			}
		}
	}

	if !changed {
		attrChanged, err := setAttributesIfDifferent(req.Path, req.Owner, req.Group, req.Mode)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
		}
		return Response{
			Changed: attrChanged,
			Path:    req.Path,
			Msg:     msg,
		}
	}

	newContent := strings.Join(lines, lineSep)
	if len(lines) > 0 {
		newContent = newContent + lineSep
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

func absent(req Request) Response {
	info, err := os.Stat(req.Path)
	if os.IsNotExist(err) {
		return Response{Changed: false, Path: req.Path, Msg: "file does not exist"}
	}
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat file: %v", err)}
	}

	if info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("path is a directory: %s", req.Path)}
	}

	content, err := os.ReadFile(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read file: %v", err)}
	}

	originalContent := string(content)
	var lineSep string = "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		lineSep = "\r\n"
	}

	lines := splitLines(string(content))

	var re *regexp.Regexp
	if req.Regexp != "" {
		var err error
		re, err = regexp.Compile(req.Regexp)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid regexp: %v", err)}
		}
	}

	var newLines []string
	removedCount := 0

	for _, line := range lines {
		keep := true

		if re != nil {
			if re.MatchString(line) {
				keep = false
			}
		} else if req.SearchString != "" {
			if strings.Contains(line, req.SearchString) {
				keep = false
			}
		} else if req.Line != "" {
			if line == req.Line {
				keep = false
			}
		}

		if keep {
			newLines = append(newLines, line)
		} else {
			removedCount++
		}
	}

	if removedCount == 0 {
		return Response{Changed: false, Path: req.Path, Msg: "no lines matched"}
	}

	newContent := strings.Join(newLines, lineSep)
	if len(newLines) > 0 {
		newContent = newContent + lineSep
	}

	var backupFile string
	if req.Backup {
		backupFile, err = createBackup(req.Path)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to create backup: %v", err)}
		}
	}

	if err := atomicWrite(req.Path, []byte(newContent)); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write file: %v", err)}
	}

	msg := fmt.Sprintf("%d line(s) removed", removedCount)
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

func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
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

func expandBackrefs(line string, matches []string) string {
	result := line
	for i := len(matches) - 1; i >= 0; i-- {
		placeholder := fmt.Sprintf("\\%d", i)
		result = strings.ReplaceAll(result, placeholder, matches[i])
	}
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
	tmpFile, err := os.CreateTemp("", "lineinfile-validate-*")
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

