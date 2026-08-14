package docker

import (
	"fmt"
	"strings"

	"github.com/docker/go-units"
)

// ParseHumanBytes parses community.docker human size strings such as "1MB",
// "128m", or a bare byte count. Empty input returns 0.
func ParseHumanBytes(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	parsed, err := units.RAMInBytes(trimmed)
	if err != nil {
		return 0, fmt.Errorf("Error while parsing value of builder_cache_keep_storage: %w", err)
	}
	return parsed, nil
}
