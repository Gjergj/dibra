package docker_image_remove

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

type removeReference struct {
	Lookup     string
	Remove     string
	Action     string
	ByID       bool
	ByDigest   bool
	Selector   string
	Repository string
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
	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(docker.WrapError("create docker client", "", err).Error())
	}
	defer apiClient.Close()
	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	image, found, err := inspectImage(ctx, apiClient, reference.Lookup)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	result := emptyResponse()
	if state.DiffMode {
		result.Diff = &Diff{Before: imageState(image, found), After: imageState(image, found)}
	}
	if !found {
		return result
	}

	result.Changed = true
	result.Actions = append(result.Actions, "Removed image "+reference.Action)
	result.Image = image
	if state.CheckMode {
		if err := predictRemoval(&result, image, reference, req.Force); err != nil {
			return failedResponse(err.Error())
		}
		return result
	}

	removed, err := apiClient.ImageRemove(ctx, reference.Remove, client.ImageRemoveOptions{
		Force: req.Force, PruneChildren: req.Prune,
	})
	if err != nil && !docker.IsNotFoundError(err) {
		return failedResponse(fmt.Sprintf("Error removing image %s - %v", reference.Action, err))
	}
	if err == nil {
		for _, item := range removed.Items {
			if item.Untagged != "" {
				result.Untagged = append(result.Untagged, item.Untagged)
			}
			if item.Deleted != "" {
				result.Deleted = append(result.Deleted, item.Deleted)
			}
		}
		sort.Strings(result.Untagged)
		sort.Strings(result.Deleted)
	}
	if result.Diff != nil {
		after, afterFound, inspectErr := inspectImage(ctx, apiClient, imageID(image))
		if inspectErr != nil {
			return failedResponse(fmt.Sprintf("Error inspecting image %s after removal - %v", imageID(image), inspectErr))
		}
		result.Diff.After = imageState(after, afterFound)
	}
	return result
}

func validateRequest(req Request) (Request, removeReference, error) {
	if req.Name == "" {
		return req, removeReference{}, fmt.Errorf("name is required")
	}
	if req.Tag == "" && (req.providedArguments == nil || !req.providedArguments["tag"]) {
		req.Tag = "latest"
	}
	if !docker.IsValidImageTag(req.Tag, true) {
		return req, removeReference{}, fmt.Errorf("%q is not a valid docker tag", req.Tag)
	}
	if req.providedArguments == nil || !req.providedArguments["prune"] {
		req.Prune = true
	}
	if docker.IsImageID(req.Name) {
		return req, removeReference{
			Lookup: req.Name, Remove: req.Name, Action: req.Name, ByID: true,
		}, nil
	}
	parsed := docker.ParseImageReference(req.Name)
	if err := parsed.Validate(); err != nil {
		return req, removeReference{}, fmt.Errorf("invalid image name %q: %v", req.Name, err)
	}
	selector := req.Tag
	byDigest := false
	if parsed.Digest != "" {
		selector = parsed.Digest
		byDigest = true
	} else if parsed.Tag != "" {
		selector = parsed.Tag
	}
	parsed.Tag, parsed.Digest = "", ""
	repository := parsed.String()
	lookup, remove, action := repository, repository, repository
	if selector != "" {
		lookup = repository + ":" + selector
		remove = lookup
		action = lookup
		if byDigest {
			lookup = repository + "@" + selector
			remove = lookup
		}
	}
	return req, removeReference{
		Lookup: lookup, Remove: remove, Action: action, ByDigest: byDigest,
		Selector: selector, Repository: repository,
	}, nil
}

func predictRemoval(result *Response, image map[string]any, reference removeReference, force bool) error {
	if reference.ByID {
		result.Deleted = append(result.Deleted, imageID(image))
		result.Untagged = append(result.Untagged, imageStrings(image, "RepoTags")...)
		result.Untagged = append(result.Untagged, imageStrings(image, "RepoDigests")...)
		sort.Strings(result.Untagged)
		if !force && len(result.Untagged) > 0 {
			return fmt.Errorf("Cannot delete image by ID that is still in use - use force=true")
		}
		if result.Diff != nil {
			result.Diff.After = ImageState{Exists: false}
		}
		return nil
	}

	result.Untagged = append(result.Untagged, reference.Action)
	tags := imageStrings(image, "RepoTags")
	digests := imageStrings(image, "RepoDigests")
	if reference.ByDigest {
		if len(tags) < 1 && len(digests) < 2 {
			result.Deleted = append(result.Deleted, imageID(image))
		}
		if result.Diff != nil {
			result.Diff.After = imageState(image, true)
			digests := removeString(*result.Diff.After.Digests, reference.Remove)
			result.Diff.After.Digests = &digests
		}
	} else {
		if len(tags) < 2 && len(digests) < 1 {
			result.Deleted = append(result.Deleted, imageID(image))
		}
		if result.Diff != nil {
			result.Diff.After = imageState(image, true)
			tags := removeString(*result.Diff.After.Tags, reference.Action)
			result.Diff.After.Tags = &tags
		}
	}
	sort.Strings(result.Deleted)
	sort.Strings(result.Untagged)
	return nil
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
		return ImageState{Exists: false}
	}
	tags := sortedCopy(imageStrings(image, "RepoTags"))
	digests := sortedCopy(imageStrings(image, "RepoDigests"))
	return ImageState{
		Exists:  true,
		ID:      imageID(image),
		Tags:    &tags,
		Digests: &digests,
	}
}

func imageID(image map[string]any) string {
	value, _ := image["Id"].(string)
	return value
}

func imageStrings(image map[string]any, key string) []string {
	values, _ := image[key].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func removeString(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func sortedCopy(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	sort.Strings(result)
	return result
}

func emptyResponse() Response {
	return Response{Actions: []string{}, Image: map[string]any{}, Deleted: []string{}, Untagged: []string{}}
}

func failedResponse(message string) Response {
	result := emptyResponse()
	result.Failed = true
	result.Msg = message
	return result
}
