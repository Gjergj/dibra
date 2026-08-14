package docker_volume

import (
	"context"
	"fmt"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

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
	req, err := normalizeRequest(req)
	if err != nil {
		return failedResponse(err.Error())
	}

	apiClient, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer apiClient.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	existing, found, err := inspectVolume(ctx, apiClient, req.volumeName())
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}

	if req.State == "absent" {
		return removeVolume(ctx, apiClient, req, found, state)
	}
	return presentVolume(ctx, apiClient, req, existing, found, state)
}

func normalizeRequest(req Request) (Request, error) {
	if req.volumeName() == "" {
		return req, fmt.Errorf("volume_name is required")
	}
	if req.State == "" && !req.argumentProvided("state") {
		req.State = "present"
	}
	if req.State != "present" && req.State != "absent" {
		return req, fmt.Errorf("state must be present or absent")
	}
	if req.Driver == "" && !req.argumentProvided("driver") {
		req.Driver = "local"
	}
	if req.DriverOptions == nil && !req.argumentProvided("driver_options") {
		req.DriverOptions = map[string]string{}
	}
	if req.Recreate == "" && !req.argumentProvided("recreate") {
		req.Recreate = "never"
	}
	if req.State == "present" && req.Recreate != "never" && req.Recreate != "always" && req.Recreate != "options-changed" {
		return req, fmt.Errorf("recreate must be never, always, or options-changed")
	}
	return req, nil
}

func presentVolume(ctx context.Context, apiClient client.APIClient, req Request, existing map[string]any, found bool, state execution.State) Response {
	differences := map[string]configDifference{}
	if found {
		differences = hasDifferentConfig(req, existing)
	}

	shouldRecreate := found && (req.Recreate == "always" || req.Recreate == "options-changed" && len(differences) > 0)
	result := Response{Volume: existing}
	if state.DiffMode {
		result.Diff = presentDiff(found, true, differences)
	}

	if shouldRecreate {
		if !state.CheckMode {
			if _, err := apiClient.VolumeRemove(ctx, req.volumeName(), client.VolumeRemoveOptions{}); err != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
			}
		}
		found = false
		existing = nil
		result.Volume = nil
		result.Changed = true
	}

	if !found {
		result.Changed = true
		if !state.CheckMode {
			options := client.VolumeCreateOptions{
				Name:       req.volumeName(),
				Driver:     req.Driver,
				DriverOpts: req.DriverOptions,
			}
			if req.Labels != nil {
				options.Labels = req.Labels
			}
			if _, err := apiClient.VolumeCreate(ctx, options); err != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
			}
			created, _, err := inspectVolume(ctx, apiClient, req.volumeName())
			if err != nil {
				return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
			}
			result.Volume = created
			return result
		}
		inspected, stillPresent, err := inspectVolume(ctx, apiClient, req.volumeName())
		if err != nil {
			return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
		}
		if stillPresent {
			result.Volume = inspected
		}
		return result
	}

	result.Volume = existing
	return result
}

func removeVolume(ctx context.Context, apiClient client.APIClient, req Request, found bool, state execution.State) Response {
	result := Response{Volume: nil}
	if state.DiffMode {
		result.Diff = presentDiff(found, false, nil)
	}
	if !found {
		return result
	}
	result.Changed = true
	if state.CheckMode {
		return result
	}
	if _, err := apiClient.VolumeRemove(ctx, req.volumeName(), client.VolumeRemoveOptions{}); err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	return result
}

type configDifference struct {
	parameter any
	active    any
}

func hasDifferentConfig(req Request, existing map[string]any) map[string]configDifference {
	differences := map[string]configDifference{}
	if req.Driver != "" {
		active, _ := existing["Driver"].(string)
		if req.Driver != active {
			differences["driver"] = configDifference{parameter: req.Driver, active: active}
		}
	}
	if len(req.DriverOptions) > 0 {
		activeOptions := stringMap(existing["Options"])
		if len(activeOptions) == 0 {
			differences["driver_options"] = configDifference{parameter: req.DriverOptions, active: existing["Options"]}
		} else {
			for key, value := range req.DriverOptions {
				active, found := activeOptions[key]
				if !found || active != value {
					var activeValue any
					if found {
						activeValue = active
					}
					differences["driver_options."+key] = configDifference{parameter: value, active: activeValue}
				}
			}
		}
	}
	if len(req.Labels) > 0 {
		activeLabels := stringMap(existing["Labels"])
		for key, value := range req.Labels {
			if activeLabels[key] != value {
				var activeValue any
				if current, found := activeLabels[key]; found {
					activeValue = current
				}
				differences["labels."+key] = configDifference{parameter: value, active: activeValue}
			}
		}
	}
	return differences
}

func presentDiff(existsBefore, existsAfter bool, differences map[string]configDifference) *Diff {
	before := map[string]any{"exists": existsBefore}
	after := map[string]any{"exists": existsAfter}
	for field, difference := range differences {
		before[field] = difference.active
		after[field] = difference.parameter
	}
	return &Diff{Before: before, After: after}
}

func inspectVolume(ctx context.Context, apiClient client.APIClient, name string) (map[string]any, bool, error) {
	result, err := apiClient.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	decoded, err := inspectMap(result)
	if err != nil {
		return nil, false, err
	}
	return decoded, true, nil
}

func inspectMap(result client.VolumeInspectResult) (map[string]any, error) {
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Volume)
}

func stringMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		result := make(map[string]string, len(typed))
		for key, item := range typed {
			if item == nil {
				continue
			}
			if text, ok := item.(string); ok {
				result[key] = text
				continue
			}
			result[key] = fmt.Sprint(item)
		}
		return result
	default:
		return map[string]string{}
	}
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
