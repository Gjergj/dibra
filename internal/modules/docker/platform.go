package docker

import (
	"fmt"
	"regexp"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var validPlatformPart = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var knownOperatingSystems = map[string]struct{}{
	"aix": {}, "android": {}, "darwin": {}, "dragonfly": {}, "freebsd": {},
	"hurd": {}, "illumos": {}, "ios": {}, "js": {}, "linux": {}, "nacl": {},
	"netbsd": {}, "openbsd": {}, "plan9": {}, "solaris": {}, "windows": {}, "zos": {},
}

var knownArchitectures = map[string]struct{}{
	"386": {}, "amd64": {}, "amd64p32": {}, "arm": {}, "armbe": {}, "arm64": {},
	"arm64be": {}, "ppc64": {}, "ppc64le": {}, "loong64": {}, "mips": {},
	"mipsle": {}, "mips64": {}, "mips64le": {}, "mips64p32": {}, "mips64p32le": {},
	"ppc": {}, "riscv": {}, "riscv64": {}, "s390": {}, "s390x": {}, "sparc": {},
	"sparc64": {}, "wasm": {},
}

// ParsePlatform matches the platform parser used by the pinned
// community.docker collection. Daemon values complete one-component OS or
// architecture inputs when the caller has daemon information available.
func ParsePlatform(value, daemonOS, daemonArchitecture string) (ocispec.Platform, error) {
	if value == "" {
		return ocispec.Platform{}, fmt.Errorf("Platform string must be non-empty")
	}
	parts := strings.SplitN(value, "/", 3)
	if len(parts) == 1 {
		if err := validatePlatformPart(value, parts[0], "OS/architecture"); err != nil {
			return ocispec.Platform{}, err
		}
		part := normalizeOperatingSystem(parts[0])
		if _, found := knownOperatingSystems[part]; found {
			architecture, variant := normalizePlatformArchitecture(daemonArchitecture, "")
			return ocispec.Platform{OS: part, Architecture: architecture, Variant: variant}, nil
		}
		architecture, variant := normalizePlatformArchitecture(part, "")
		if _, found := knownArchitectures[architecture]; found {
			return ocispec.Platform{
				OS:           normalizeOperatingSystem(daemonOS),
				Architecture: architecture,
				Variant:      variant,
			}, nil
		}
		return ocispec.Platform{}, fmt.Errorf(
			"Invalid platform string %q: unknown OS or architecture", value)
	}

	if err := validatePlatformPart(value, parts[0], "OS"); err != nil {
		return ocispec.Platform{}, err
	}
	if err := validatePlatformPart(value, parts[1], "architecture"); err != nil {
		return ocispec.Platform{}, err
	}
	variant := ""
	if len(parts) == 3 {
		if err := validatePlatformPart(value, parts[2], "variant"); err != nil {
			return ocispec.Platform{}, err
		}
		variant = parts[2]
	}
	architecture, variant := normalizePlatformArchitecture(parts[1], variant)
	if len(parts) == 2 && architecture == "arm" && variant == "v7" {
		variant = ""
	}
	if len(parts) == 3 && architecture == "arm64" && variant == "" {
		variant = "v8"
	}
	return ocispec.Platform{
		OS:           normalizeOperatingSystem(parts[0]),
		Architecture: architecture,
		Variant:      variant,
	}, nil
}

// ComposePlatform normalizes platform fields returned by the daemon and fills
// omitted OS or architecture fields from daemon information.
func ComposePlatform(
	operatingSystem,
	architecture,
	variant,
	daemonOS,
	daemonArchitecture string,
) ocispec.Platform {
	if operatingSystem == "" {
		operatingSystem = daemonOS
	}
	if architecture == "" {
		architecture = daemonArchitecture
	}
	architecture, variant = normalizePlatformArchitecture(architecture, variant)
	return ocispec.Platform{
		OS:           normalizeOperatingSystem(operatingSystem),
		Architecture: architecture,
		Variant:      variant,
	}
}

func validatePlatformPart(value, part, name string) error {
	if part == "" {
		return fmt.Errorf("Invalid platform string %q: %s is empty", value, name)
	}
	if !validPlatformPart.MatchString(part) {
		return fmt.Errorf("Invalid platform string %q: %s has invalid characters", value, name)
	}
	return nil
}

func normalizeOperatingSystem(value string) string {
	value = strings.ToLower(value)
	if value == "macos" {
		return "darwin"
	}
	return value
}

func normalizePlatformArchitecture(architecture, variant string) (string, string) {
	architecture = strings.ToLower(architecture)
	variant = strings.ToLower(variant)
	switch architecture {
	case "i386":
		return "386", ""
	case "x86_64", "x86-64", "amd64":
		if variant == "v1" {
			variant = ""
		}
		return "amd64", variant
	case "aarch64", "arm64":
		if variant == "8" || variant == "v8" {
			variant = ""
		}
		return "arm64", variant
	case "armhf":
		return "arm", "v7"
	case "armel":
		return "arm", "v6"
	case "arm":
		switch variant {
		case "":
			return "arm", "v7"
		case "5", "6", "7", "8":
			return "arm", "v" + variant
		}
	}
	return architecture, variant
}
