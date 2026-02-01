package replace

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

	if req.Regexp == "" {
		return Response{Failed: true, Msg: "regexp is required"}
	}

	info, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Response{Failed: true, Msg: fmt.Sprintf("path does not exist: %s", req.Path)}
		}
		return Response{Failed: true, Msg: fmt.Sprintf("failed to stat path: %v", err)}
	}

	if info.IsDir() {
		return Response{Failed: true, Msg: fmt.Sprintf("path is a directory: %s", req.Path)}
	}

	content, err := os.ReadFile(req.Path)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read file: %v", err)}
	}
	originalContent := string(content)

	re, err := regexp.Compile("(?m)" + req.Regexp)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid regexp: %v", err)}
	}

	var afterRe, beforeRe *regexp.Regexp
	if req.After != "" {
		afterRe, err = regexp.Compile("(?s)" + req.After)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid after regexp: %v", err)}
		}
	}
	if req.Before != "" {
		beforeRe, err = regexp.Compile("(?s)" + req.Before)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("invalid before regexp: %v", err)}
		}
	}

	var newContent string
	var replaceCount int

	if afterRe != nil || beforeRe != nil {
		newContent, replaceCount = replaceInSection(originalContent, re, req.Replace, afterRe, beforeRe)
	} else {
		newContent, replaceCount = replaceAll(originalContent, re, req.Replace)
	}

	if newContent == originalContent {
		attrChanged, err := setAttributesIfDifferent(req.Path, req.Owner, req.Group, req.Mode)
		if err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
		}
		if replaceCount == 0 && (afterRe != nil || beforeRe != nil) {
			return Response{
				Changed: attrChanged,
				Msg:     fmt.Sprintf("Pattern for before/after params did not match the given file: after=%q, before=%q", req.After, req.Before),
			}
		}
		return Response{Changed: attrChanged, Msg: ""}
	}

	if req.Validate != "" {
		if err := validateContent(req.Validate, []byte(newContent)); err != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("validation failed: %v", err)}
		}
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

	if _, err := setAttributesIfDifferent(req.Path, req.Owner, req.Group, req.Mode); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to set attributes: %v", err)}
	}

	return Response{
		Changed:    true,
		Msg:        fmt.Sprintf("%d replacements made", replaceCount),
		BackupFile: backupFile,
		Diff: &Diff{
			Before: originalContent,
			After:  newContent,
		},
	}
}

func replaceAll(content string, re *regexp.Regexp, replacement string) (string, int) {
	matches := re.FindAllStringIndex(content, -1)
	count := len(matches)
	goReplacement := convertBackrefsToGo(replacement)
	newContent := re.ReplaceAllString(content, goReplacement)
	return newContent, count
}

func convertBackrefsToGo(replacement string) string {
	result := replacement

	explicitBackrefRe := regexp.MustCompile(`\\g<(\d+)>`)
	result = explicitBackrefRe.ReplaceAllString(result, "$${$1}")

	for i := 9; i >= 0; i-- {
		result = strings.ReplaceAll(result, fmt.Sprintf("\\%d", i), fmt.Sprintf("${%d}", i))
	}

	return result
}

func replaceInSection(content string, re *regexp.Regexp, replacement string, afterRe, beforeRe *regexp.Regexp) (string, int) {
	startIdx := 0
	endIdx := len(content)

	if afterRe != nil && beforeRe != nil {
		pattern := regexp.MustCompile("(?s)" + afterRe.String() + "(.*?)" + beforeRe.String())
		match := pattern.FindStringSubmatchIndex(content)
		if match == nil {
			return content, 0
		}
		startIdx = match[2]
		endIdx = match[3]
	} else if afterRe != nil {
		match := afterRe.FindStringIndex(content)
		if match == nil {
			return content, 0
		}
		startIdx = match[1]
	} else if beforeRe != nil {
		match := beforeRe.FindStringIndex(content)
		if match == nil {
			return content, 0
		}
		endIdx = match[0]
	}

	section := content[startIdx:endIdx]
	matches := re.FindAllStringIndex(section, -1)
	count := len(matches)
	goReplacement := convertBackrefsToGo(replacement)
	newSection := re.ReplaceAllString(section, goReplacement)

	return content[:startIdx] + newSection + content[endIdx:], count
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
	tmpFile, err := os.CreateTemp("", "replace-validate-*")
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
