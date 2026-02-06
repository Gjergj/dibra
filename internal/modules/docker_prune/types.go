package docker_prune

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Containers          bool              `json:"containers"`
	ContainersFilters   map[string]string `json:"containers_filters"` // Filter containers to prune
	Images              bool              `json:"images"`             // dangling by default, or all if all=true (via filters)
	ImagesFilters       map[string]string `json:"images_filters"`     // e.g. {"dangling": "false"} for all
	Networks            bool              `json:"networks"`
	NetworksFilters     map[string]string `json:"networks_filters"` // Filter networks to prune
	Volumes             bool              `json:"volumes"`
	VolumesFilters      map[string]string `json:"volumes_filters"` // Filter volumes to prune
	Builder             bool              `json:"builder"`
	BuilderCacheAll     bool              `json:"builder_cache_all"`     // Remove all build cache, not just dangling
	BuilderCacheFilters map[string]string `json:"builder_cache_filters"` // Filter build cache to prune
}

type Response struct {
	Changed             bool     `json:"changed"`
	Failed              bool     `json:"failed"`
	Msg                 string   `json:"msg,omitempty"`
	ContainersDeleted   []string `json:"containers_deleted,omitempty"`
	ImagesDeleted       []string `json:"images_deleted,omitempty"`
	NetworksDeleted     []string `json:"networks_deleted,omitempty"`
	VolumesDeleted      []string `json:"volumes_deleted,omitempty"`
	BuildCacheItemsUsed int      `json:"build_cache_items_used,omitempty"` // Number of build cache items pruned
	SpaceReclaimed      uint64   `json:"space_reclaimed"`
}
