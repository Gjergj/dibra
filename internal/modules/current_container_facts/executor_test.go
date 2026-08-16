package current_container_facts

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type memoryFS struct {
	docker.FileSystem
	files      map[string][]byte
	readErrors map[string]error
}

func (fileSystem memoryFS) ReadFile(path string) ([]byte, error) {
	if err := fileSystem.readErrors[path]; err != nil {
		return nil, err
	}
	data, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (fileSystem memoryFS) Stat(path string) (fs.FileInfo, error) {
	data, found := fileSystem.files[path]
	if !found {
		if _, hasReadError := fileSystem.readErrors[path]; !hasReadError {
			return nil, fs.ErrNotExist
		}
	}
	return procFileInfo{size: int64(len(data))}, nil
}

type procFileInfo struct {
	size int64
}

func (info procFileInfo) Name() string       { return "proc" }
func (info procFileInfo) Size() int64        { return info.size }
func (info procFileInfo) Mode() fs.FileMode  { return 0o444 }
func (info procFileInfo) ModTime() time.Time { return time.Time{} }
func (info procFileInfo) IsDir() bool        { return false }
func (info procFileInfo) Sys() any           { return nil }

func facts(files map[string][]byte) Response {
	return ExecuteWithDependencies(Request{}, docker.Dependencies{FileSystem: memoryFS{files: files}})
}

func TestFactsDetectDockerFromCpuset(t *testing.T) {
	id := strings.Repeat("a", 64)
	response := facts(map[string][]byte{
		"/proc/self/cpuset": []byte("/docker/" + id + "\n"),
	})
	if response.Failed || response.Changed || response.AnsibleFacts["ansible_module_container_type"] != "docker" {
		t.Fatalf("response = %#v", response)
	}
	if response.AnsibleFacts["ansible_module_running_in_container"] != true || response.AnsibleFacts["ansible_module_container_id"] != id {
		t.Fatalf("facts = %#v", response.AnsibleFacts)
	}
}

func TestFactsDetectGitHubActionsAndAzure(t *testing.T) {
	id := strings.Repeat("b", 64)
	github := facts(map[string][]byte{"/proc/self/cpuset": []byte("/actions_job/" + id)})
	if github.AnsibleFacts["ansible_module_container_type"] != "github_actions" {
		t.Fatalf("github = %#v", github.AnsibleFacts)
	}
	azure := facts(map[string][]byte{"/proc/self/cpuset": []byte("/azpl_job/" + id)})
	if azure.AnsibleFacts["ansible_module_container_type"] != "azure_pipelines" {
		t.Fatalf("azure = %#v", azure.AnsibleFacts)
	}
}

func TestFactsFallsBackToMountinfoForDockerAndPodman(t *testing.T) {
	id := strings.Repeat("c", 64)
	dockerFacts := facts(map[string][]byte{
		"/proc/self/cpuset":    []byte("/\n"),
		"/proc/self/mountinfo": []byte("1 0 0:0 / / rw - overlay overlay rw\n22 1 0:21 /docker/" + id + "/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n"),
	})
	if dockerFacts.AnsibleFacts["ansible_module_container_type"] != "docker" || dockerFacts.AnsibleFacts["ansible_module_container_id"] != id {
		t.Fatalf("docker mountinfo = %#v", dockerFacts.AnsibleFacts)
	}

	podmanID := strings.Repeat("d", 64)
	podman := facts(map[string][]byte{
		"/proc/self/mountinfo": []byte("22 1 0:21 /containers/storage/" + podmanID + "/userdata/hostname /etc/hostname rw - ext4 /dev/sda1 rw\n"),
	})
	if podman.AnsibleFacts["ansible_module_container_type"] != "podman" || podman.AnsibleFacts["ansible_module_container_id"] != podmanID {
		t.Fatalf("podman = %#v", podman.AnsibleFacts)
	}
}

func TestFactsOutsideContainer(t *testing.T) {
	response := facts(map[string][]byte{
		"/proc/self/cpuset":    []byte("/\n"),
		"/proc/self/mountinfo": []byte("1 0 0:0 / / rw - overlay overlay rw\n"),
	})
	if response.AnsibleFacts["ansible_module_running_in_container"] != false || response.AnsibleFacts["ansible_module_container_id"] != "" || response.AnsibleFacts["ansible_module_container_type"] != "" {
		t.Fatalf("facts = %#v", response.AnsibleFacts)
	}
}

func TestFactsFailWhenExistingProcFileCannotBeRead(t *testing.T) {
	for _, filename := range []string{"/proc/self/cpuset", "/proc/self/mountinfo"} {
		t.Run(filename, func(t *testing.T) {
			files := map[string][]byte{}
			if filename == "/proc/self/mountinfo" {
				files["/proc/self/cpuset"] = []byte("/\n")
			}
			response := ExecuteWithDependencies(Request{}, docker.Dependencies{FileSystem: memoryFS{
				files:      files,
				readErrors: map[string]error{filename: errors.New("permission denied")},
			}})
			if !response.Failed || !strings.Contains(response.Msg, "failed to read "+filename) {
				t.Fatalf("response = %#v", response)
			}
		})
	}
}

func TestFactsFailOnInvalidUTF8(t *testing.T) {
	response := facts(map[string][]byte{"/proc/self/cpuset": {0xff}})
	if !response.Failed || !strings.Contains(response.Msg, "UTF-8") {
		t.Fatalf("response = %#v", response)
	}
}
