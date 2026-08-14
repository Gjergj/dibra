package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const SupportedComposeVersion = "5.4.0"

// ComposeEvent is the normalized subset of a Compose JSON progress event used
// for change detection and error reporting.
type ComposeEvent struct {
	ResourceType string
	ResourceID   string
	Status       string
	Message      string
}

type ComposeEventResult struct {
	Events   []ComposeEvent
	Warnings []string
}

type composeProgressLine struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Level    string `json:"level"`
	Message  string `json:"message"`
	Msg      string `json:"msg"`
	Tail     bool   `json:"tail"`
	Error    bool   `json:"error"`
}

var composeKnownStatuses = map[string]bool{
	"Built": true, "Building": true, "Created": true, "Creating": true,
	"Done": true, "Error": true, "Exited": true, "Healthy": true,
	"Killed": true, "Killing": true, "Pulled": true, "Pulling": true,
	"Recreate": true, "Recreated": true, "Removed": true, "Removing": true,
	"Restarted": true, "Restarting": true, "Running": true, "Skipped": true,
	"Started": true, "Starting": true, "Stopped": true, "Stopping": true,
	"Waiting": true, "Warning": true, "Working": true,
}

var composeResourceTypes = map[string]string{
	"Container": "container",
	"Image":     "image",
	"Network":   "network",
	"Service":   "service",
	"Volume":    "volume",
}

// ParseComposeJSONEvents parses the JSON progress format used by the supported
// Compose 5.4 baseline. It also understands the older JSON field arrangement
// that Compose 5 still emits for several operations.
func ParseComposeJSONEvents(output []byte) ComposeEventResult {
	result := ComposeEventResult{}
	for _, rawLine := range bytes.Split(output, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte{'{'}) {
			if bytes.HasPrefix(line, []byte("Warning: ")) {
				result.Warnings = append(result.Warnings, string(bytes.TrimSpace(line[len("Warning: "):])))
			}
			continue
		}

		var progress composeProgressLine
		if err := json.Unmarshal(line, &progress); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("cannot parse Compose JSON event %q: %v", line, err))
			continue
		}
		if progress.Level == "warning" {
			message := progress.Msg
			if message == "" {
				message = progress.Message
			}
			result.Warnings = append(result.Warnings, message)
			continue
		}
		if progress.Tail {
			status := "Error"
			message := progress.Text
			if strings.HasPrefix(strings.ToLower(message), "warning:") {
				status = "Warning"
				message = strings.TrimSpace(message[len("warning:"):])
			}
			result.Events = append(result.Events, ComposeEvent{ResourceType: "unknown", Status: status, Message: message})
			continue
		}
		if progress.Error || progress.Level == "error" {
			message := progress.Message
			if message == "" {
				message = progress.Msg
			}
			result.Events = append(result.Events, ComposeEvent{ResourceType: "unknown", ResourceID: progress.ID, Status: "Error", Message: message})
			continue
		}

		event := ComposeEvent{ResourceType: "unknown", ResourceID: progress.ID, Status: progress.Status, Message: progress.Text}
		if event.Message == "" {
			event.Message = progress.Message
		}
		if event.Message == "" {
			event.Message = progress.Msg
		}
		if (progress.Status == "Working" || progress.Status == "Done") && strings.HasPrefix(progress.ParentID, "Image ") {
			event.ResourceType = "image-layer"
			event.ResourceID = strings.TrimPrefix(progress.ParentID, "Image ")
		} else if resourceType, resourceID, found := strings.Cut(progress.ID, " "); found {
			if normalized, known := composeResourceTypes[resourceType]; known {
				event.ResourceType = normalized
				event.ResourceID = resourceID
			}
		} else if progress.Text == "Pulling" || progress.Text == "Pulled" {
			event.ResourceType = "image"
		} else if isComposeLayerStatus(progress.Text) {
			event.ResourceType = "image-layer"
		}

		if ((event.Status == "Working" || event.Status == "Done") && event.ResourceType != "image-layer" && composeKnownStatuses[event.Message]) ||
			(!composeKnownStatuses[event.Status] && composeKnownStatuses[event.Message]) {
			event.Status, event.Message = event.Message, event.Status
		}
		if event.Status == "" && strings.HasPrefix(event.Message, "Skipped - ") {
			event.Status = "Skipped"
			event.Message = strings.TrimPrefix(event.Message, "Skipped - ")
		}
		result.Events = append(result.Events, event)
	}
	return result
}

func isComposeLayerStatus(status string) bool {
	switch status {
	case "Already exists", "Download complete", "Downloading", "Extracting", "Pull complete", "Pulling fs layer", "Verifying Checksum", "Waiting":
		return true
	default:
		return false
	}
}

var composeWorkingStatuses = map[string]bool{
	"Building": true, "Creating": true, "Killing": true, "Pulling": true,
	"Recreate": true, "Removing": true, "Restarting": true, "Starting": true,
	"Stopping": true,
}

var composePullStatuses = map[string]bool{"Pulled": true, "Pulling": true}
var composeBuildStatuses = map[string]bool{"Built": true, "Building": true}
var composeErrorStatuses = map[string]bool{"Error": true}
var composePullProgressWorking = map[string]bool{
	"Downloading": true, "Extracting": true, "Pulling fs layer": true,
	"Verifying Checksum": true, "Waiting": true, "Working": true,
}

// ComposeChangeOptions controls which Compose progress events count as a change.
type ComposeChangeOptions struct {
	IgnoreServicePullEvents bool
	IgnoreBuildEvents       bool
}

// ComposeAction is the upstream `{what,id,status}` action record.
type ComposeAction struct {
	What   string `json:"what"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

func ComposeEventsChanged(events []ComposeEvent) bool {
	return ComposeHasChanges(events, ComposeChangeOptions{})
}

func ComposeHasChanges(events []ComposeEvent, options ComposeChangeOptions) bool {
	for _, event := range events {
		if composeWorkingStatuses[event.Status] {
			if options.IgnoreServicePullEvents && composePullStatuses[event.Status] {
				continue
			}
			if options.IgnoreBuildEvents && composeBuildStatuses[event.Status] {
				continue
			}
			return true
		}
		if event.ResourceType == "image-layer" && composePullProgressWorking[event.Status] {
			return true
		}
	}
	return false
}

func ComposeEventActions(events []ComposeEvent) []string {
	records := ComposeEventActionRecords(events)
	result := make([]string, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		action := strings.TrimSpace(strings.Join([]string{record.What, record.ID, record.Status}, " "))
		if !seen[action] {
			seen[action] = true
			result = append(result, action)
		}
	}
	return result
}

func ComposeEventActionRecords(events []ComposeEvent) []ComposeAction {
	actions := make([]ComposeAction, 0)
	pullSeen := map[string]bool{}
	for _, event := range events {
		if event.ResourceType == "image-layer" && composePullProgressWorking[event.Status] {
			key := event.ResourceID + "\x00" + event.Status
			if !pullSeen[key] {
				pullSeen[key] = true
				actions = append(actions, ComposeAction{What: event.ResourceType, ID: event.ResourceID, Status: event.Status})
			}
		}
		if event.ResourceType != "image-layer" && composeWorkingStatuses[event.Status] {
			actions = append(actions, ComposeAction{What: event.ResourceType, ID: event.ResourceID, Status: event.Status})
		}
	}
	return actions
}

func ComposeFailureMessage(events []ComposeEvent, rc int) string {
	var errors []string
	for _, event := range events {
		if !composeErrorStatuses[event.Status] {
			continue
		}
		message := event.Status
		if event.Message != "" {
			message = event.Message
		}
		prefix := "General error: "
		switch {
		case event.ResourceType != "unknown" && event.ResourceID != "":
			prefix = fmt.Sprintf("Error when processing %s %s: ", event.ResourceType, event.ResourceID)
		case event.ResourceType != "unknown":
			prefix = fmt.Sprintf("Error when processing %s: ", event.ResourceType)
		case event.ResourceID != "":
			prefix = fmt.Sprintf("Error when processing %s: ", event.ResourceID)
		}
		errors = append(errors, prefix+message)
	}
	if len(errors) == 0 {
		return fmt.Sprintf("Return code %d is non-zero", rc)
	}
	return strings.Join(errors, "\n")
}

// ValidateComposeVersion accepts only the pinned Compose baseline. Moving to
// another release requires updating the baseline after a compatibility review.
func ValidateComposeVersion(version string) error {
	actual, err := parseNumericVersion(version)
	if err != nil {
		return err
	}
	supported, _ := parseNumericVersion(SupportedComposeVersion)
	if actual != supported {
		return fmt.Errorf("unsupported Docker Compose version %s; Dibra requires the pinned version %s", version, SupportedComposeVersion)
	}
	return nil
}

func parseNumericVersion(version string) ([3]int, error) {
	var result [3]int
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	version = strings.SplitN(version, "-", 2)[0]
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return result, fmt.Errorf("invalid Docker Compose version %q", version)
	}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return result, fmt.Errorf("invalid Docker Compose version %q", version)
		}
		result[index] = value
	}
	return result, nil
}

// CheckComposeVersion performs the supported-baseline check through the
// injected Docker CLI runner.
func CheckComposeVersion(ctx context.Context, runner CLIRunner, common CommonArgs, environment Environment) (string, error) {
	return CheckComposeVersionWithCLI(ctx, runner, common, environment, "docker")
}

func CheckComposeVersionWithCLI(ctx context.Context, runner CLIRunner, common CommonArgs, environment Environment, dockerCLI string) (string, error) {
	if dockerCLI == "" {
		dockerCLI = "docker"
	}
	args, err := DockerCLIArgsWithEnvironment(common, environment, "compose", "version", "--format", "json")
	if err != nil {
		return "", err
	}
	commandEnvironment, err := DockerCLIEnvWithEnvironment(common, environment)
	if err != nil {
		return "", err
	}
	result, err := runner.Run(ctx, CLICommand{Name: dockerCLI, Args: args, Env: commandEnvironment})
	if err != nil {
		return "", fmt.Errorf("query Docker Compose version: %w", err)
	}
	var response struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(result.Output, &response); err != nil || response.Version == "" {
		return "", fmt.Errorf("parse Docker Compose version output %q", strings.TrimSpace(string(result.Output)))
	}
	if err := ValidateComposeVersion(response.Version); err != nil {
		return "", err
	}
	return strings.TrimPrefix(response.Version, "v"), nil
}
