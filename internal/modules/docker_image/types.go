package docker_image

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Name        string `json:"name"`
	Tag         string `json:"tag"`
	Repository  string `json:"repository"` // For tagging/pushing
	State       string `json:"state"`      // present, absent
	Source      string `json:"source"`     // pull, local, build, load (default: pull)
	ForceSource bool   `json:"force_source"`
	Push        bool   `json:"push"`
	ArchivePath string `json:"archive_path"` // For load
	DockerFile  string `json:"dockerfile"`   // For build
	BuildPath   string `json:"build.path"`   // For build context
	ForceTag    bool   `json:"force_tag"`
	KeepImage   bool   `json:"keep_image"` // For build/load?
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	ImageID string `json:"image_id,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}
