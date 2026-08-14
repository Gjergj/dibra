package current_container_facts

import (
	"path"
	"regexp"
	"strings"

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
	containerID, containerType := detectContainer(dependencies.FileSystem)
	return Response{
		AnsibleFacts: map[string]any{
			"ansible_module_running_in_container": containerID != "",
			"ansible_module_container_id":         containerID,
			"ansible_module_container_type":       containerType,
		},
	}
}

func detectContainer(fileSystem docker.FileSystem) (string, string) {
	if data, err := fileSystem.ReadFile("/proc/self/cpuset"); err == nil {
		cgroupPath, cgroupName := path.Split(strings.TrimSpace(string(data)))
		cgroupPath = strings.TrimSuffix(cgroupPath, "/")
		switch cgroupPath {
		case "/docker":
			return cgroupName, "docker"
		case "/azpl_job":
			return cgroupName, "azure_pipelines"
		case "/actions_job":
			return cgroupName, "github_actions"
		}
	}

	data, err := fileSystem.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 5 || parts[4] != "/etc/hostname" {
			continue
		}
		if match := dockerHostnamePath.FindStringSubmatch(parts[3]); len(match) == 2 {
			return match[1], "docker"
		}
		if match := podmanHostnamePath.FindStringSubmatch(parts[3]); len(match) == 2 {
			return match[1], "podman"
		}
	}
	return "", ""
}
