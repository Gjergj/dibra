package docker_prune

import "github.com/gjergjiramku/dibra/internal/modules/docker"

type Request struct {
	docker.CommonArgs

	Containers              bool             `json:"containers"`
	ContainersFilters       docker.FilterMap `json:"containers_filters"`
	Images                  bool             `json:"images"`
	ImagesFilters           docker.FilterMap `json:"images_filters"`
	Networks                bool             `json:"networks"`
	NetworksFilters         docker.FilterMap `json:"networks_filters"`
	Volumes                 bool             `json:"volumes"`
	VolumesFilters          docker.FilterMap `json:"volumes_filters"`
	BuilderCache            bool             `json:"builder_cache"`
	BuilderCacheAll         bool             `json:"builder_cache_all"`
	BuilderCacheFilters     docker.FilterMap `json:"builder_cache_filters"`
	BuilderCacheKeepStorage string           `json:"builder_cache_keep_storage"`
}

func (request Request) pruneBuilderCache() bool {
	return request.BuilderCache
}

type Response struct {
	Changed                    bool              `json:"changed"`
	Failed                     bool              `json:"failed"`
	Msg                        string            `json:"msg,omitempty"`
	Containers                 *[]string         `json:"containers,omitempty"`
	ContainersSpaceReclaimed   *uint64           `json:"containers_space_reclaimed,omitempty"`
	Images                     *[]map[string]any `json:"images,omitempty"`
	ImagesSpaceReclaimed       *uint64           `json:"images_space_reclaimed,omitempty"`
	Networks                   *[]string         `json:"networks,omitempty"`
	Volumes                    *[]string         `json:"volumes,omitempty"`
	VolumesSpaceReclaimed      *uint64           `json:"volumes_space_reclaimed,omitempty"`
	BuilderCacheSpaceReclaimed *uint64           `json:"builder_cache_space_reclaimed,omitempty"`
	BuilderCacheCachesDeleted  *[]string         `json:"builder_cache_caches_deleted,omitempty"`
}
