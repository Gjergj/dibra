package docker_prune

import "github.com/gjergjiramku/goansible/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Containers    bool              `json:"containers"`
	Images        bool              `json:"images"` // dangling by default, or all if all=true (via filters? Ansible has distinct option)
	Networks      bool              `json:"networks"`
	Volumes       bool              `json:"volumes"`
	Builder       bool              `json:"builder"`
	ImagesFilters map[string]string `json:"images_filters"` // e.g. {"dangling": "false"} for all
}

type Response struct {
	Changed           bool     `json:"changed"`
	Failed            bool     `json:"failed"`
	Msg               string   `json:"msg,omitempty"`
	ContainersDeleted []string `json:"containers_deleted,omitempty"`
	ImagesDeleted     []string `json:"images_deleted,omitempty"`
	NetworksDeleted   []string `json:"networks_deleted,omitempty"`
	VolumesDeleted    []string `json:"volumes_deleted,omitempty"`
	SpaceReclaimed    uint64   `json:"space_reclaimed"`
}
