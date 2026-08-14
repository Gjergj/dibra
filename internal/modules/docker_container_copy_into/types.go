package docker_container_copy_into

import (
	"encoding/json"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

type Request struct {
	docker.CommonArgs

	Container          string          `json:"container"`
	Path               *string         `json:"path"`
	Content            *string         `json:"content"`
	ContentIsB64       bool            `json:"content_is_b64"`
	ContainerPath      string          `json:"container_path"`
	Follow             bool            `json:"follow"`
	LocalFollow        *bool           `json:"local_follow"`
	OwnerID            *int            `json:"owner_id"`
	GroupID            *int            `json:"group_id"`
	Mode               json.RawMessage `json:"mode"`
	ModeParse          string          `json:"mode_parse"`
	Force              *bool           `json:"force"`
	MaxFileSizeForDiff *int            `json:"_max_file_size_for_diff"`
}

type Response struct {
	Changed       bool           `json:"changed"`
	Failed        bool           `json:"failed"`
	Msg           string         `json:"msg,omitempty"`
	ContainerPath string         `json:"container_path,omitempty"`
	Diff          map[string]any `json:"diff,omitempty"`
}
