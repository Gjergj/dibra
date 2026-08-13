package docker_image_push

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

type pushReference struct {
	Repository string
	Reference  string
	Registry   string
	Path       string
}

func Execute(req Request) Response {
	return ExecuteWithDependencies(req, docker.Dependencies{})
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	dependencies = dependencies.Resolve()
	req, reference, err := validateRequest(req)
	if err != nil {
		return failedResponse(err.Error())
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(docker.WrapError("create docker client", "", err).Error())
	}
	defer apiClient.Close()
	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	image, found, err := inspectImage(ctx, apiClient, reference.Reference)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if !found {
		return failedResponse(fmt.Sprintf("Cannot find image %s", reference.Reference))
	}

	auth, err := docker.ResolveRegistryAuthForImage(reference.Reference, dependencies, true)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error resolving registry authentication - %v", err))
	}
	stream, err := apiClient.ImagePush(ctx, reference.Reference, client.ImagePushOptions{RegistryAuth: auth})
	if err != nil {
		return pushFailure(reference, err)
	}
	defer stream.Close()
	parsed := docker.ParsePullPushStream(stream)
	if parsed.Error != nil {
		return pushFailure(reference, parsed.Error)
	}

	changed := false
	for _, line := range parsed.Logs {
		if strings.HasSuffix(line, ": Pushing") || strings.HasSuffix(line, ": Pushed") ||
			line == "Pushing" || line == "Pushed" {
			changed = true
		}
	}
	return Response{
		Changed: changed,
		Actions: []string{"Pushed image " + reference.Reference},
		Image:   image,
	}
}

func validateRequest(req Request) (Request, pushReference, error) {
	if req.Name == "" {
		return req, pushReference{}, fmt.Errorf("name is required")
	}
	if isImageID(req.Name) {
		return req, pushReference{}, fmt.Errorf("Cannot push an image by ID")
	}
	if req.Tag == "" && (req.providedArguments == nil || !req.providedArguments["tag"]) {
		req.Tag = "latest"
	}
	if !validTag(req.Tag, true) {
		return req, pushReference{}, fmt.Errorf("%q is not a valid docker tag!", req.Tag)
	}

	parsed := docker.ParseImageReference(req.Name)
	if parsed.Digest != "" {
		return req, pushReference{}, fmt.Errorf("Cannot push an image by digest")
	}
	tag := req.Tag
	if parsed.Tag != "" {
		tag = parsed.Tag
	}
	if !validTag(tag, false) {
		return req, pushReference{}, fmt.Errorf("%q is not a valid docker tag!", tag)
	}
	parsed.Tag = ""
	if err := parsed.Validate(); err != nil {
		return req, pushReference{}, fmt.Errorf("invalid image name %q: %v", req.Name, err)
	}
	repository := parsed.String()
	registryName, err := docker.RegistryName(repository)
	if err != nil {
		return req, pushReference{}, err
	}
	return req, pushReference{
		Repository: repository,
		Reference:  repository + ":" + tag,
		Registry:   registryName,
		Path:       parsed.Path,
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

func pushFailure(reference pushReference, err error) Response {
	message := err.Error()
	target := reference.Registry + "/" + reference.Path + strings.TrimPrefix(reference.Reference, reference.Repository)
	if strings.Contains(strings.ToLower(message), "unauthorized") {
		if strings.Contains(strings.ToLower(message), "authentication required") {
			return failedResponse(fmt.Sprintf("Error pushing image %s - %v. Try logging into %s first.", target, err, reference.Registry))
		}
		return failedResponse(fmt.Sprintf("Error pushing image %s - %v. Does the repository exist?", target, err))
	}
	return failedResponse(fmt.Sprintf("Error pushing image %s: %v", reference.Reference, err))
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

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Actions: []string{}, Image: map[string]any{}}
}
