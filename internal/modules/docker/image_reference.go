package docker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	imagePathPattern   = regexp.MustCompile(`^[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*(\/[a-z0-9]+((\.|_|__|-+)[a-z0-9]+)*)*$`)
	imageTagPattern    = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	imageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-fA-F]{64}$`)
)

// ImageReference is the parsed form of a Docker image reference. It mirrors
// the public community.docker naming behavior without adding an implicit tag.
type ImageReference struct {
	Registry string
	Path     string
	Tag      string
	Digest   string
}

// ParseImageReference separates registry, repository path, tag, and digest.
// A first path component is a registry only when it contains a dot or colon,
// or is exactly localhost.
func ParseImageReference(value string) ImageReference {
	result := ImageReference{}

	if index := strings.LastIndex(value, "@"); index >= 0 {
		result.Digest = value[index+1:]
		value = value[:index]
	}
	if index := strings.LastIndex(value, ":"); index >= 0 && !strings.Contains(value[index+1:], "/") {
		result.Tag = value[index+1:]
		value = value[:index]
	}
	if parts := strings.SplitN(value, "/", 2); len(parts) == 2 &&
		(strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		result.Registry = parts[0]
		result.Path = parts[1]
	} else {
		result.Path = value
	}

	return result
}

// Validate checks the image-reference components accepted by the pinned
// community.docker baseline.
func (reference ImageReference) Validate() error {
	if reference.Registry != "" {
		if strings.HasPrefix(reference.Registry, "-") || strings.HasSuffix(reference.Registry, "-") {
			return fmt.Errorf("invalid registry name (%s): must not begin or end with a hyphen", reference.Registry)
		}
		if strings.HasSuffix(reference.Registry, ":") {
			return fmt.Errorf("invalid registry name (%s): must not end with a colon", reference.Registry)
		}
	}
	if !imagePathPattern.MatchString(reference.Path) {
		return fmt.Errorf("invalid image path (%s)", reference.Path)
	}
	if reference.Tag != "" && !imageTagPattern.MatchString(reference.Tag) {
		return fmt.Errorf("invalid image tag (%s)", reference.Tag)
	}
	if reference.Digest != "" && !imageDigestPattern.MatchString(reference.Digest) {
		return fmt.Errorf("invalid image digest (%s)", reference.Digest)
	}
	return nil
}

// Normalize canonicalizes Docker Hub registry aliases and inserts the
// library namespace for single-component Docker Hub repositories.
func (reference ImageReference) Normalize() ImageReference {
	result := reference
	switch result.Registry {
	case "", "index.docker.io", "registry.hub.docker.com":
		result.Registry = "docker.io"
	}
	if result.Registry == "docker.io" && result.Path != "" && !strings.Contains(result.Path, "/") {
		result.Path = "library/" + result.Path
	}
	return result
}

// String reconstructs the reference without changing its components.
func (reference ImageReference) String() string {
	var result strings.Builder
	if reference.Registry != "" {
		result.WriteString(reference.Registry)
		if reference.Path != "" {
			result.WriteByte('/')
		}
	}
	result.WriteString(reference.Path)
	if reference.Tag != "" {
		result.WriteByte(':')
		result.WriteString(reference.Tag)
	}
	if reference.Digest != "" {
		result.WriteByte('@')
		result.WriteString(reference.Digest)
	}
	return result.String()
}

// HostnamePort returns the registry endpoint of a normalized reference.
func (reference ImageReference) HostnamePort() (string, int, error) {
	if reference.Registry == "" {
		return "", 0, fmt.Errorf("cannot get registry hostname before normalization")
	}
	if reference.Registry == "docker.io" {
		return "index.docker.io", 443, nil
	}
	hostname, portText, found := strings.Cut(reference.Registry, ":")
	if !found {
		return reference.Registry, 443, nil
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("cannot parse registry port %q", portText)
	}
	return hostname, port, nil
}

// NormalizeImageReference validates and canonicalizes an image reference.
func NormalizeImageReference(value string) (string, error) {
	reference := ParseImageReference(value)
	if err := reference.Validate(); err != nil {
		return "", err
	}
	return reference.Normalize().String(), nil
}

// JoinImageNameTag applies tag only when name does not already contain a tag
// or digest, matching the focused image modules' option precedence.
func JoinImageNameTag(name, tag string) (string, error) {
	reference := ParseImageReference(name)
	if reference.Tag == "" && reference.Digest == "" && tag != "" {
		reference.Tag = tag
	}
	if err := reference.Validate(); err != nil {
		return "", err
	}
	return reference.String(), nil
}
