package current_container_facts

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

var (
	dockerHostnamePath = regexp.MustCompile(`.*/([a-f0-9]{64})/hostname$`)
	podmanHostnamePath = regexp.MustCompile(`.*/([a-f0-9]{64})/userdata/hostname$`)
)

type Request struct{}

type Response struct {
	Changed      bool           `json:"changed"`
	Failed       bool           `json:"failed"`
	Msg          string         `json:"msg,omitempty"`
	AnsibleFacts map[string]any `json:"ansible_facts"`
}

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(_ Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	containerID, containerType, err := detectContainer(dependencies.FileSystem)
	if err != nil {
		return Response{Failed: true, Msg: err.Error()}
	}
	return Response{
		AnsibleFacts: map[string]any{
			"ansible_module_running_in_container": containerID != "",
			"ansible_module_container_id":         containerID,
			"ansible_module_container_type":       containerType,
		},
	}
}

func detectContainer(fileSystem docker.FileSystem) (string, string, error) {
	if data, exists, err := readProcFile(fileSystem, "/proc/self/cpuset"); err != nil {
		return "", "", err
	} else if exists {
		cgroupPath, cgroupName := path.Split(strings.TrimSpace(string(data)))
		cgroupPath = strings.TrimSuffix(cgroupPath, "/")
		switch cgroupPath {
		case "/docker":
			return cgroupName, "docker", nil
		case "/azpl_job":
			return cgroupName, "azure_pipelines", nil
		case "/actions_job":
			return cgroupName, "github_actions", nil
		}
	}

	data, exists, err := readProcFile(fileSystem, "/proc/self/mountinfo")
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 5 || parts[4] != "/etc/hostname" {
			continue
		}
		if match := dockerHostnamePath.FindStringSubmatch(parts[3]); len(match) == 2 {
			return match[1], "docker", nil
		}
		if match := podmanHostnamePath.FindStringSubmatch(parts[3]); len(match) == 2 {
			return match[1], "podman", nil
		}
	}
	return "", "", nil
}

func readProcFile(fileSystem docker.FileSystem, filename string) ([]byte, bool, error) {
	if _, err := fileSystem.Stat(filename); err != nil {
		// Python's os.path.exists() returns false for inaccessible paths.
		return nil, false, nil
	}
	data, err := fileSystem.ReadFile(filename)
	if err != nil {
		return nil, true, fmt.Errorf("failed to read %s: %w", filename, err)
	}
	if !utf8.Valid(data) {
		return nil, true, fmt.Errorf("failed to decode %s as UTF-8", filename)
	}
	return data, true, nil
}
