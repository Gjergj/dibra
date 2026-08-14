package docker_context_info

import (
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type memoryFile struct {
	data []byte
	dir  bool
}

type memoryFS struct {
	docker.FileSystem
	home  string
	files map[string]memoryFile
}

func (fileSystem memoryFS) UserHomeDir() (string, error) { return fileSystem.home, nil }

func (fileSystem memoryFS) ReadFile(path string) ([]byte, error) {
	file, found := fileSystem.files[path]
	if !found || file.dir {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), file.data...), nil
}

func (fileSystem memoryFS) Stat(path string) (fs.FileInfo, error) {
	file, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return memoryInfo{name: filepath.Base(path), size: int64(len(file.data)), dir: file.dir}, nil
}

func (fileSystem memoryFS) WalkDir(root string, walk fs.WalkDirFunc) error {
	matched := false
	for path, file := range fileSystem.files {
		if path != root && !isUnder(root, path) {
			continue
		}
		matched = true
		info := memoryInfo{name: filepath.Base(path), size: int64(len(file.data)), dir: file.dir}
		if err := walk(path, info, nil); err != nil {
			return err
		}
	}
	if !matched {
		return walk(root, nil, fs.ErrNotExist)
	}
	return nil
}

func isUnder(root, path string) bool {
	for current := path; current != "." && current != "/"; current = filepath.Dir(current) {
		if current == root {
			return true
		}
	}
	return false
}

type memoryInfo struct {
	name string
	size int64
	dir  bool
}

func (info memoryInfo) Name() string { return info.name }
func (info memoryInfo) Size() int64  { return info.size }
func (info memoryInfo) Mode() fs.FileMode {
	if info.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (info memoryInfo) ModTime() time.Time         { return time.Time{} }
func (info memoryInfo) IsDir() bool                { return info.dir }
func (info memoryInfo) Sys() any                   { return nil }
func (info memoryInfo) Type() fs.FileMode          { return info.Mode().Type() }
func (info memoryInfo) Info() (fs.FileInfo, error) { return info, nil }

func contextDependencies(fileSystem memoryFS, env docker.StaticEnvironment) docker.Dependencies {
	return docker.Dependencies{FileSystem: fileSystem, Environment: env}
}

func TestContextInfoListsDefaultAndNamedContexts(t *testing.T) {
	id := contextID("desktop")
	fileSystem := memoryFS{
		home: "/home/user",
		files: map[string]memoryFile{
			"/home/user/.docker/config.json": {data: []byte(`{"currentContext":"desktop"}`)},
			"/home/user/.docker/contexts/meta/" + id + "/meta.json": {data: []byte(`{
				"Name":"desktop",
				"Metadata":{"Description":"Docker Desktop"},
				"Endpoints":{"docker":{"Host":"unix:///var/run/docker.sock","SkipTLSVerify":false}}
			}`)},
		},
	}
	response := ExecuteWithDependencies(Request{}, contextDependencies(fileSystem, docker.StaticEnvironment{}))
	if response.Failed || response.CurrentContextName != "desktop" || len(response.Contexts) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Contexts[0].Name != "default" || response.Contexts[0].MetaPath != nil {
		t.Fatalf("default = %#v", response.Contexts[0])
	}
	if response.Contexts[0].Description != defaultDescription {
		t.Fatalf("default description = %q", response.Contexts[0].Description)
	}
	desktop := response.Contexts[1]
	if !desktop.Current || desktop.Config["docker_host"] != "unix:///var/run/docker.sock" || desktop.Config["tls"] != false {
		t.Fatalf("desktop = %#v", desktop)
	}
}

func TestContextInfoDOCKERHOSTSelectsDefault(t *testing.T) {
	fileSystem := memoryFS{
		home: "/home/user",
		files: map[string]memoryFile{
			"/home/user/.docker/config.json": {data: []byte(`{"currentContext":"desktop"}`)},
		},
	}
	response := ExecuteWithDependencies(Request{}, contextDependencies(fileSystem, docker.StaticEnvironment{
		"DOCKER_HOST": "tcp://127.0.0.1:2375",
	}))
	if response.Failed || response.CurrentContextName != "default" {
		t.Fatalf("response = %#v", response)
	}
	if response.Contexts[0].Config["docker_host"] != "tcp://127.0.0.1:2375" {
		t.Fatalf("default host = %#v", response.Contexts[0].Config)
	}
}

func TestContextInfoMissingNamedContextFails(t *testing.T) {
	fileSystem := memoryFS{home: "/home/user", files: map[string]memoryFile{}}
	response := ExecuteWithDependencies(Request{Name: "missing"}, contextDependencies(fileSystem, docker.StaticEnvironment{}))
	if !response.Failed || response.Msg == "" {
		t.Fatalf("response = %#v", response)
	}
}

func TestContextInfoOnlyCurrentAndNameAreExclusive(t *testing.T) {
	response := ExecuteWithDependencies(Request{OnlyCurrent: true, Name: "default"}, contextDependencies(memoryFS{home: "/home/user", files: map[string]memoryFile{}}, docker.StaticEnvironment{}))
	if !response.Failed {
		t.Fatalf("response = %#v", response)
	}
}

func TestContextInfoCLIContextOverride(t *testing.T) {
	fileSystem := memoryFS{home: "/home/user", files: map[string]memoryFile{}}
	response := ExecuteWithDependencies(Request{CLIContext: "default", OnlyCurrent: true}, contextDependencies(fileSystem, docker.StaticEnvironment{
		"DOCKER_CONTEXT": "other",
	}))
	if response.Failed || response.CurrentContextName != "default" || len(response.Contexts) != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestContextInfoNormalizesHTTPHost(t *testing.T) {
	id := contextID("remote")
	fileSystem := memoryFS{
		home: "/home/user",
		files: map[string]memoryFile{
			"/home/user/.docker/contexts/meta/" + id + "/meta.json": {data: []byte(`{
				"Name":"remote",
				"Endpoints":{"docker":{"Host":"http://127.0.0.1:2375","SkipTLSVerify":true}}
			}`)},
		},
	}
	response := ExecuteWithDependencies(Request{Name: "remote"}, contextDependencies(fileSystem, docker.StaticEnvironment{}))
	if response.Failed || response.Contexts[0].Config["docker_host"] != "tcp://127.0.0.1:2375" || response.Contexts[0].Config["tls"] != true {
		t.Fatalf("response = %#v", response)
	}
}
