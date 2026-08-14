package docker_host_info

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Containers        bool             `json:"containers"`
	ContainersAll     bool             `json:"containers_all"`
	ContainersFilters docker.FilterMap `json:"containers_filters"`
	Images            bool             `json:"images"`
	ImagesFilters     docker.FilterMap `json:"images_filters"`
	Networks          bool             `json:"networks"`
	NetworksFilters   docker.FilterMap `json:"networks_filters"`
	Volumes           bool             `json:"volumes"`
	VolumesFilters    docker.FilterMap `json:"volumes_filters"`
	DiskUsage         bool             `json:"disk_usage"`
	VerboseOutput     bool             `json:"verbose_output"`
}

type Response struct {
	Changed         bool              `json:"changed"`
	Failed          bool              `json:"failed"`
	Msg             string            `json:"msg,omitempty"`
	CanTalkToDocker bool              `json:"can_talk_to_docker"`
	HostInfo        map[string]any    `json:"host_info,omitempty"`
	Containers      *[]map[string]any `json:"containers,omitempty"`
	Images          *[]map[string]any `json:"images,omitempty"`
	Networks        *[]map[string]any `json:"networks,omitempty"`
	Volumes         *[]map[string]any `json:"volumes,omitempty"`
	DiskUsage       map[string]any    `json:"disk_usage,omitempty"`
}
