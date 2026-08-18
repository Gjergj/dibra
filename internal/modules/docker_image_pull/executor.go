package docker_image_pull

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type pullReference struct {
	Repository string
	Reference  string
	Action     string
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
	req, reference, err := validateRequest(req)
	if err != nil {
		return failedResponse(err.Error())
	}
	if req.Platform != "" {
		version := effectiveAPIVersion(req.CommonArgs, dependencies.Environment)
		if version != "auto" && !apiVersionAtLeast(version, 1, 32) {
			return failedResponse(fmt.Sprintf("option platform requires Docker API version 1.32 or newer; configured version is %s", version))
		}
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(docker.WrapError("create docker client", "", err).Error())
	}
	defer apiClient.Close()
	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	before, found, err := inspectImage(ctx, apiClient, reference.Reference)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	result := Response{
		Actions: []string{},
		Image:   before,
		Diff: Diff{
			Before: imageState(before, found),
			After:  imageState(before, found),
		},
	}

	var platform *ocispec.Platform
	if req.Platform != "" {
		resolved, err := resolvePlatform(ctx, apiClient, req.Platform)
		if err != nil {
			return failedResponse(err.Error())
		}
		platform = &resolved
	}
	if found && req.Pull == "not_present" &&
		(platform == nil || imageMatchesPlatform(before, *platform)) {
		return result
	}

	result.Actions = append(result.Actions, "Pulled image "+reference.Action)
	if state.CheckMode {
		result.Changed = true
		result.Diff.After = ImageState{ID: "unknown"}
		return result
	}

	auth, err := docker.ResolveRegistryAuthForImageContext(ctx, reference.Reference, dependencies, false)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error resolving registry authentication - %v", err))
	}
	options := client.ImagePullOptions{RegistryAuth: auth}
	if platform != nil {
		options.Platforms = append(options.Platforms, *platform)
	}
	stream, err := apiClient.ImagePull(ctx, reference.Reference, options)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error pulling image %s - %v", reference.Reference, err))
	}
	defer stream.Close()
	parsed := docker.ParsePullPushStream(stream)
	if parsed.Error != nil {
		return failedResponse(fmt.Sprintf("Error pulling image %s - %v", reference.Reference, parsed.Error))
	}

	after, afterFound, err := inspectImage(ctx, apiClient, reference.Reference)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error inspecting image %s after pull - %v", reference.Reference, err))
	}
	if !afterFound {
		return failedResponse(fmt.Sprintf("Error pulling image %s - image was not present after pull", reference.Reference))
	}
	result.Image = after
	result.Changed = !found || !imagesEqual(before, after)
	result.Diff.After = imageState(after, true)
	return result
}

func validateRequest(req Request) (Request, pullReference, error) {
	if req.Name == "" {
		return req, pullReference{}, fmt.Errorf("name is required")
	}
	if isImageID(req.Name) {
		return req, pullReference{}, fmt.Errorf("Cannot pull an image by ID")
	}
	if req.Tag == "" && (req.providedArguments == nil || !req.providedArguments["tag"]) {
		req.Tag = "latest"
	}
	if !validTag(req.Tag, true) {
		return req, pullReference{}, fmt.Errorf("%q is not a valid docker tag!", req.Tag)
	}
	if req.Pull == "" && (req.providedArguments == nil || !req.providedArguments["pull"]) {
		req.Pull = "always"
	}
	if req.Pull != "always" && req.Pull != "not_present" {
		return req, pullReference{}, fmt.Errorf("pull must be one of always or not_present")
	}

	parsed := docker.ParseImageReference(req.Name)
	if err := parsed.Validate(); err != nil {
		return req, pullReference{}, fmt.Errorf("invalid image name %q: %v", req.Name, err)
	}
	selector := req.Tag
	if parsed.Digest != "" {
		selector = parsed.Digest
	} else if parsed.Tag != "" {
		selector = parsed.Tag
	}
	parsed.Tag = ""
	digest := parsed.Digest
	parsed.Digest = ""
	repository := parsed.String()
	apiSelector := selector
	if apiSelector == "" {
		apiSelector = "latest"
	}
	reference := repository + ":" + apiSelector
	if digest != "" {
		reference = repository + "@" + digest
	}
	return req, pullReference{
		Repository: repository,
		Reference:  reference,
		Action:     repository + ":" + selector,
	}, nil
}

func inspectImage(ctx context.Context, apiClient client.APIClient, reference string) (map[string]any, bool, error) {
	inspection, err := apiClient.ImageInspect(ctx, reference)
	if err != nil {
		if docker.IsNotFoundError(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, err
	}
	encoded, err := json.Marshal(inspection)
	if err != nil {
		return nil, false, err
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, false, err
	}
	return raw, true, nil
}

func imageState(image map[string]any, found bool) ImageState {
	if !found {
		exists := false
		return ImageState{Exists: &exists}
	}
	return ImageState{ID: imageID(image)}
}

func imageID(image map[string]any) string {
	value, _ := image["Id"].(string)
	return value
}

func imagesEqual(first, second map[string]any) bool {
	filtered := func(image map[string]any) map[string]any {
		result := make(map[string]any, len(image))
		for key, value := range image {
			if key != "Metadata" && key != "Identity" {
				result[key] = value
			}
		}
		return result
	}
	return reflect.DeepEqual(filtered(first), filtered(second))
}

func resolvePlatform(ctx context.Context, apiClient client.APIClient, value string) (ocispec.Platform, error) {
	if !strings.Contains(value, "/") {
		info, err := apiClient.Info(ctx, client.InfoOptions{})
		if err != nil {
			return ocispec.Platform{}, docker.WrapError("inspect Docker daemon platform", "", err)
		}
		return docker.ParsePlatform(value, info.Info.OSType, info.Info.Architecture)
	}
	return docker.ParsePlatform(value, "", "")
}

func imageMatchesPlatform(image map[string]any, wanted ocispec.Platform) bool {
	operatingSystem, _ := image["Os"].(string)
	architecture, _ := image["Architecture"].(string)
	variant, _ := image["Variant"].(string)
	current := docker.ComposePlatform(operatingSystem, architecture, variant, "", "")
	return current.OS == wanted.OS &&
		current.Architecture == wanted.Architecture &&
		current.Variant == wanted.Variant
}

func isImageID(value string) bool {
	return docker.IsImageID(value)
}

func validTag(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if len(value) > 128 || value[0] == '.' || value[0] == '-' {
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

func failedResponse(message string) Response {
	return Response{
		Failed:  true,
		Msg:     message,
		Actions: []string{},
		Image:   map[string]any{},
		Diff:    Diff{},
	}
}
