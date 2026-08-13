package docker_image_export

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type requestedImage struct {
	Joined string
	ID     string
}

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	if len(req.Names) == 0 {
		return Response{Failed: true, Msg: "At least one image name must be specified", Images: []map[string]any{}}
	}
	if req.Path == "" {
		return Response{Failed: true, Msg: "path is required", Images: []map[string]any{}}
	}
	if req.Tag == "" {
		req.Tag = "latest"
	}
	if !validTag(req.Tag) {
		return Response{Failed: true, Msg: fmt.Sprintf("%q is not a valid docker tag", req.Tag), Images: []map[string]any{}}
	}

	var platform *ocispec.Platform
	if req.Platform != "" {
		parsed, err := parsePlatform(req.Platform)
		if err != nil {
			return Response{Failed: true, Msg: err.Error(), Images: []map[string]any{}}
		}
		if version := effectiveAPIVersion(req.CommonArgs, dependencies.Environment); version != "auto" && !apiVersionAtLeast(version, 1, 48) {
			return Response{Failed: true, Msg: fmt.Sprintf("option platform requires Docker API version 1.48 or newer; configured version is %s", version), Images: []map[string]any{}}
		}
		platform = &parsed
	}

	client, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return Response{Failed: true, Msg: fmt.Sprintf("failed to create docker client: %v", err), Images: []map[string]any{}}
	}
	defer client.Close()
	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	requested := make([]requestedImage, 0, len(req.Names))
	images := make([]map[string]any, 0, len(req.Names))
	exportNames := make([]string, 0, len(req.Names))
	for _, name := range req.Names {
		joined := name
		if !isImageID(name) {
			joined, err = docker.JoinImageNameTag(name, req.Tag)
			if err != nil {
				return Response{Failed: true, Msg: fmt.Sprintf("invalid image name %q: %v", name, err), Images: images}
			}
		}
		inspection, inspectErr := client.ImageInspect(ctx, joined)
		if inspectErr != nil {
			return Response{Failed: true, Msg: fmt.Sprintf("Image %s not found", joined), Images: images}
		}
		raw, conversionErr := inspectionMap(inspection)
		if conversionErr != nil {
			return Response{Failed: true, Msg: conversionErr.Error(), Images: images}
		}
		requested = append(requested, requestedImage{Joined: joined, ID: inspection.ID})
		images = append(images, raw)
		exportNames = append(exportNames, joined)
	}

	result := Response{Images: images}
	reason := exportReason(req, dependencies, requested)
	if reason == "" {
		return result
	}
	result.Changed = true
	result.Msg = reason
	if state.CheckMode {
		return result
	}

	var archive io.ReadCloser
	if platform == nil {
		archive, err = client.ImageSave(ctx, exportNames)
	} else {
		archive, err = client.ImageSave(ctx, exportNames, mobyclient.ImageSaveWithPlatforms(*platform))
	}
	if err != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error getting image%s %s - %v", pluralSuffix(len(exportNames)), strings.Join(exportNames, ", "), err)
		return result
	}
	defer archive.Close()

	file, err := dependencies.FileSystem.Create(req.Path)
	if err != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error writing image archive %s - %v", req.Path, err)
		return result
	}
	_, copyErr := io.Copy(file, archive)
	closeErr := file.Close()
	if copyErr != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error writing image archive %s - %v", req.Path, copyErr)
		return result
	}
	if closeErr != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Error writing image archive %s - %v", req.Path, closeErr)
	}
	return result
}

func exportReason(req Request, dependencies docker.Dependencies, requested []requestedImage) string {
	if req.Force {
		return "Exporting since force=true"
	}
	file, err := dependencies.FileSystem.Open(req.Path)
	if err != nil {
		return "Overwriting since no image is present in archive"
	}
	manifest, manifestErr := docker.ReadImageArchiveManifest(file)
	_ = file.Close()
	if manifestErr != nil {
		return "Overwriting an unreadable archive file"
	}

	remaining := append([]requestedImage(nil), requested...)
	for _, archived := range manifest {
		found := false
		for index, desired := range remaining {
			if desired.ID == docker.ArchiveImageID(archived.ImageID) &&
				len(archived.RepoTags) == 1 && archived.RepoTags[0] == desired.Joined {
				remaining = append(remaining[:index], remaining[index+1:]...)
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("Overwriting archive since it contains unexpected image %s named %s", archived.ImageID, strings.Join(archived.RepoTags, ", "))
		}
	}
	if len(remaining) > 0 {
		names := make([]string, len(remaining))
		for index, image := range remaining {
			names[index] = image.Joined
		}
		return fmt.Sprintf("Overwriting archive since it is missing image(s) %s", strings.Join(names, ", "))
	}
	return ""
}

func inspectionMap(value mobyclient.ImageInspectResult) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode image inspection: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("decode image inspection: %w", err)
	}
	return result, nil
}

func isImageID(value string) bool {
	trimmed := strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if len(trimmed) < 12 || len(trimmed) > 64 {
		return false
	}
	for _, character := range trimmed {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func validTag(value string) bool {
	if value == "" || len(value) > 128 || value[0] == '.' || value[0] == '-' {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func parsePlatform(value string) (ocispec.Platform, error) {
	parts := strings.Split(value, "/")
	if len(parts) < 1 || len(parts) > 3 || parts[0] == "" {
		return ocispec.Platform{}, fmt.Errorf("invalid platform %q: expected os[/architecture[/variant]]", value)
	}
	result := ocispec.Platform{OS: parts[0]}
	if len(parts) > 1 {
		if parts[1] == "" {
			return ocispec.Platform{}, fmt.Errorf("invalid platform %q: architecture is empty", value)
		}
		result.Architecture = parts[1]
	}
	if len(parts) > 2 {
		if parts[2] == "" {
			return ocispec.Platform{}, fmt.Errorf("invalid platform %q: variant is empty", value)
		}
		result.Variant = parts[2]
	}
	return result, nil
}

func effectiveAPIVersion(common docker.CommonArgs, environment docker.Environment) string {
	if common.APIVersion != nil {
		return *common.APIVersion
	}
	if value, found := environment.LookupEnv("DOCKER_API_VERSION"); found {
		return value
	}
	return "auto"
}

func apiVersionAtLeast(value string, minimumMajor, minimumMinor int) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major > minimumMajor || major == minimumMajor && minor >= minimumMinor
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
