package docker_image_tag

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

type tagTarget struct {
	Name      string
	Tag       string
	Reference string
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
	req, sourceReference, targets, err := validateRequest(req)
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

	source, found, err := inspectImage(ctx, apiClient, sourceReference)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if !found {
		return failedResponse(fmt.Sprintf("Cannot find image %s", sourceReference))
	}
	result := Response{
		Actions:      []string{},
		Image:        source,
		TaggedImages: []string{},
		Diff: Diff{
			Before: DiffImages{Images: []ImageState{}},
			After:  DiffImages{Images: []ImageState{}},
		},
	}

	for _, target := range targets {
		existing, targetFound, err := inspectImage(ctx, apiClient, target.Reference)
		if err != nil {
			return failedResponse(fmt.Sprintf("Error inspecting target image %s - %v", target.Reference, err))
		}
		result.Diff.Before.Images = append(result.Diff.Before.Images, tagState(target, existing, targetFound))

		sourceID := imageID(source)
		existingID := imageID(existing)
		changed := false
		message := ""
		after := existing
		afterFound := targetFound
		switch {
		case targetFound && existingID == sourceID:
			message = fmt.Sprintf("target image already exists (%s) and is as expected", existingID)
		case targetFound && req.ExistingImages == "keep":
			message = fmt.Sprintf("target image already exists (%s) and is not as expected, but kept", existingID)
		default:
			changed = true
			if targetFound {
				message = fmt.Sprintf("target image existed (%s) and was not as expected", existingID)
			} else {
				message = "target image did not exist"
			}
			if !state.CheckMode {
				if _, err := apiClient.ImageTag(ctx, client.ImageTagOptions{Source: sourceID, Target: target.Reference}); err != nil {
					return failedResponse(fmt.Sprintf("Error: failed to tag image as %s:%s - %v", target.Name, target.Tag, err))
				}
			}
			after, afterFound = source, true
		}

		if changed {
			result.Changed = true
			result.Actions = append(result.Actions,
				fmt.Sprintf("Tagged image %s as %s:%s: %s", sourceID, target.Name, target.Tag, message))
			result.TaggedImages = append(result.TaggedImages, target.Name+":"+target.Tag)
		} else {
			result.Actions = append(result.Actions,
				fmt.Sprintf("Not tagged image %s as %s:%s: %s", sourceID, target.Name, target.Tag, message))
		}
		result.Diff.After.Images = append(result.Diff.After.Images, tagState(target, after, afterFound))
	}
	return result
}

func validateRequest(req Request) (Request, string, []tagTarget, error) {
	if req.Name == "" {
		return req, "", nil, fmt.Errorf("name is required")
	}
	if req.Tag == "" && (req.providedArguments == nil || !req.providedArguments["tag"]) {
		req.Tag = "latest"
	}
	if !docker.IsValidImageTag(req.Tag, true) {
		return req, "", nil, fmt.Errorf("%q is not a valid docker tag", req.Tag)
	}
	if req.ExistingImages == "" && (req.providedArguments == nil || !req.providedArguments["existing_images"]) {
		req.ExistingImages = "overwrite"
	}
	if req.ExistingImages != "keep" && req.ExistingImages != "overwrite" {
		return req, "", nil, fmt.Errorf("existing_images must be one of keep or overwrite")
	}
	if req.Repository == nil {
		return req, "", nil, fmt.Errorf("repository is required")
	}

	sourceReference := req.Name
	if !docker.IsImageID(req.Name) {
		parsed := docker.ParseImageReference(req.Name)
		if err := parsed.Validate(); err != nil {
			return req, "", nil, fmt.Errorf("invalid image name %q: %v", req.Name, err)
		}
		if parsed.Tag == "" && parsed.Digest == "" {
			parsed.Tag = req.Tag
		}
		sourceReference = parsed.String()
	}

	targets := make([]tagTarget, 0, len(req.Repository))
	for index, value := range req.Repository {
		if docker.IsImageID(value) {
			return req, "", nil, fmt.Errorf("repository[%d] must not be an image ID; got: %s", index+1, value)
		}
		parsed := docker.ParseImageReference(value)
		if parsed.Digest != "" {
			return req, "", nil, fmt.Errorf("repository[%d] must not have a digest; got: %s", index+1, value)
		}
		targetTag := parsed.Tag
		if targetTag == "" {
			targetTag = req.Tag
		} else if !docker.IsValidImageTag(targetTag, false) {
			return req, "", nil, fmt.Errorf("repository[%d] must not have a digest; got: %s", index+1, value)
		}
		parsed.Tag = ""
		if err := parsed.Validate(); err != nil {
			return req, "", nil, fmt.Errorf("invalid repository[%d] %q: %v", index+1, value, err)
		}
		name := parsed.String()
		reference := name
		if targetTag != "" {
			reference += ":" + targetTag
		}
		targets = append(targets, tagTarget{Name: name, Tag: targetTag, Reference: reference})
	}
	return req, sourceReference, targets, nil
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

func tagState(target tagTarget, image map[string]any, found bool) ImageState {
	result := ImageState{Name: target.Name, Tag: target.Tag}
	if !found {
		exists := false
		result.Exists = &exists
		return result
	}
	result.ID = imageID(image)
	return result
}

func imageID(image map[string]any) string {
	value, _ := image["Id"].(string)
	return value
}

func failedResponse(message string) Response {
	return Response{
		Failed: true, Msg: message, Actions: []string{}, Image: map[string]any{},
		TaggedImages: []string{},
		Diff:         Diff{Before: DiffImages{Images: []ImageState{}}, After: DiffImages{Images: []ImageState{}}},
	}
}
