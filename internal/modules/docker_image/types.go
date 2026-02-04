package docker_image

import (
	"encoding/json"

	"github.com/gjergjiramku/goansible/internal/modules/docker"
)

// PullPolicy represents the image pull behavior with backward compatibility
type PullPolicy string

const (
	PullMissing PullPolicy = "missing"
	PullAlways  PullPolicy = "always"
	PullNever   PullPolicy = "never"
)

// UnmarshalJSON handles both bool and string for backward compatibility
func (p *PullPolicy) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = PullPolicy(s)
		return nil
	}

	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if b {
			*p = PullAlways
		} else {
			*p = PullMissing
		}
		return nil
	}

	*p = PullMissing
	return nil
}

type Request struct {
	docker.CommonArgs

	Name       string `json:"name"`
	Tag        string `json:"tag"`
	Repository string `json:"repository"` // For tagging/pushing to a different repo
	State      string `json:"state"`      // present, absent
	Source     string `json:"source"`     // pull, local, build, load (default: pull)

	// Pull behavior (3.2)
	Pull PullPolicy `json:"pull"` // missing, always, never (default: missing)

	// Force flags (3.5) - split from ForceSource for clarity
	ForcePull   bool `json:"force_pull"`   // Force pull even if image exists (deprecated, use pull: always)
	ForceRemove bool `json:"force_remove"` // Force remove (removes containers using the image)
	ForceTag    bool `json:"force_tag"`    // Force tag even if target exists

	// Legacy support
	ForceSource bool `json:"force_source"` // Deprecated: use force_pull or force_remove

	// Push options (3.4)
	Push bool `json:"push"` // Push after tagging

	// Registry authentication (3.1)
	RegistryUsername string `json:"registry_username"` // Username for registry auth
	RegistryPassword string `json:"registry_password"` // Password for registry auth

	// Build options (for source=build)
	ArchivePath string `json:"archive_path"` // For load
	DockerFile  string `json:"dockerfile"`   // For build
	BuildPath   string `json:"build.path"`   // For build context

	// Other options
	KeepImage bool `json:"keep_image"` // For build/load: keep intermediate images
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	ImageID string `json:"image_id,omitempty"` // Full image ID (sha256:...)
	Digest  string `json:"digest,omitempty"`   // Image digest from registry
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}
