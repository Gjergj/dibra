package apt_repository

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	sourcesListDir = "/etc/apt/sources.list.d"
	aptGetCmd      = "/usr/bin/apt-get"
)

var validSourceTypes = map[string]struct{}{
	"deb":     {},
	"deb-src": {},
}

var envVars = []string{
	"DEBIAN_FRONTEND=noninteractive",
	"LANG=C.UTF-8",
	"LC_ALL=C.UTF-8",
}

func Execute(req Request) Response {
	if req.State == "" {
		req.State = "present"
	}

	if req.Repo == "" {
		return Response{Failed: true, Msg: "repo is required"}
	}

	if err := validateSource(req.Repo); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("invalid repository string: %v", err)}
	}

	switch req.State {
	case "present":
		return addRepo(req)
	case "absent":
		return removeRepo(req)
	default:
		return Response{Failed: true, Msg: fmt.Sprintf("unknown state: %s", req.State)}
	}
}

// validateSource validates the one-line sources.list format documented by
// sources.list(5). It intentionally does not validate whether the URI is
// reachable or whether the suite/component exists on the remote server; apt
// remains responsible for those checks during cache updates.
func validateSource(source string) error {
	parts := strings.Fields(source)
	if len(parts) == 0 {
		return fmt.Errorf("source is empty")
	}

	if _, ok := validSourceTypes[parts[0]]; !ok {
		return fmt.Errorf("source type %q is not supported; expected deb or deb-src", parts[0])
	}

	remainingParts := parts[1:]
	if len(remainingParts) > 0 && strings.HasPrefix(remainingParts[0], "[") {
		if strings.HasSuffix(remainingParts[0], "]") {
			remainingParts = remainingParts[1:]
		} else {
			endBracketIndex := -1
			for i, part := range remainingParts[1:] {
				if strings.HasSuffix(part, "]") {
					endBracketIndex = i + 1
					break
				}
			}
			if endBracketIndex == -1 {
				return fmt.Errorf("repository options must be enclosed in brackets")
			}
			remainingParts = remainingParts[endBracketIndex+1:]
		}
	}

	if len(remainingParts) < 2 {
		return fmt.Errorf("source must include a URI and suite")
	}

	// A path-style suite ending in '/' is complete without a component. A
	// normal distribution suite requires at least one component.
	suite := remainingParts[1]
	if strings.HasSuffix(suite, "/") {
		if len(remainingParts) > 2 {
			return fmt.Errorf("path-style suites ending in / cannot include components")
		}
	} else if len(remainingParts) < 3 {
		return fmt.Errorf("source must include at least one component")
	}

	return nil
}

func addRepo(req Request) Response {
	filename := req.Filename
	if filename == "" {
		filename = suggestFilename(req.Repo)
	}
	if !strings.HasSuffix(filename, ".list") {
		filename += ".list"
	}

	filepath := filepath.Join(sourcesListDir, filename)

	if repoExists(filepath, req.Repo) {
		return Response{Changed: false, Msg: "repository already present", Filename: filename}
	}

	if err := os.MkdirAll(sourcesListDir, 0755); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create sources.list.d: %v", err)}
	}

	content := req.Repo + "\n"

	existing, _ := os.ReadFile(filepath)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		content = "\n" + content
	}

	var finalContent []byte
	if len(existing) > 0 {
		finalContent = append(existing, []byte(content)...)
	} else {
		finalContent = []byte(content)
	}

	if err := os.WriteFile(filepath, finalContent, 0644); err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to write source file: %v", err)}
	}

	resp := Response{Changed: true, Repo: req.Repo, Filename: filename, Msg: fmt.Sprintf("repository added to %s", filepath)}

	if req.UpdateCache {
		rc, stdout, stderr := updateCache()
		resp.RC = rc
		resp.Stdout = stdout
		resp.Stderr = stderr
		if rc != 0 {
			resp.Failed = true
			resp.Msg = "repository added but cache update failed"
		}
	}

	return resp
}

func removeRepo(req Request) Response {
	var found bool
	var removedFrom string

	entries, err := os.ReadDir(sourcesListDir)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to read sources.list.d: %v", err)}
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".list") {
			continue
		}

		filepath := filepath.Join(sourcesListDir, entry.Name())
		content, err := os.ReadFile(filepath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		var newLines []string
		modified := false

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == req.Repo || trimmed == "# "+req.Repo {
				modified = true
				found = true
				removedFrom = filepath
				continue
			}
			newLines = append(newLines, line)
		}

		if modified {
			newContent := strings.Join(newLines, "\n")
			if strings.TrimSpace(newContent) == "" {
				os.Remove(filepath)
			} else {
				if err := os.WriteFile(filepath, []byte(newContent), 0644); err != nil {
					return Response{Failed: true, Msg: fmt.Sprintf("failed to write source file: %v", err)}
				}
			}
		}
	}

	if !found {
		return Response{Changed: false, Msg: "repository not found"}
	}

	resp := Response{Changed: true, Repo: req.Repo, Msg: fmt.Sprintf("repository removed from %s", removedFrom)}

	if req.UpdateCache {
		rc, stdout, stderr := updateCache()
		resp.RC = rc
		resp.Stdout = stdout
		resp.Stderr = stderr
	}

	return resp
}

func suggestFilename(repo string) string {
	re := regexp.MustCompile(`https?://([^/]+)`)
	matches := re.FindStringSubmatch(repo)

	var name string
	if len(matches) > 1 {
		name = matches[1]
	} else {
		name = repo
	}

	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, ":", "_")

	re = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	name = re.ReplaceAllString(name, "")

	if len(name) > 50 {
		name = name[:50]
	}

	return name
}

func repoExists(filepath, repo string) bool {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == repo {
			return true
		}
	}

	return false
}

func updateCache() (int, string, string) {
	cmd := exec.Command(aptGetCmd, "update")
	cmd.Env = append(os.Environ(), envVars...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	rc := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc = exitErr.ExitCode()
		} else {
			rc = 1
		}
	}

	return rc, stdout.String(), stderr.String()
}
