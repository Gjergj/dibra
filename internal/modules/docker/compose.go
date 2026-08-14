package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

var ComposeFileNames = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// ComposeCommonArgs common arguments for Docker Compose modules
type ComposeCommonArgs struct {
	ProjectSrc         string            `json:"project_src"`
	ProjectName        string            `json:"project_name"`
	Files              []string          `json:"files"`
	Definition         map[string]any    `json:"definition"`
	EnvFiles           []string          `json:"env_files"`
	Env                map[string]string `json:"env"`
	Profiles           []string          `json:"profiles"`
	CheckFilesExisting *bool             `json:"check_files_existing"`
}

// GetComposeBaseArgs returns the base CLI arguments for docker compose.
func GetComposeBaseArgs(args ComposeCommonArgs, common CommonArgs) ([]string, error) {
	return GetComposeBaseArgsWithEnvironment(args, common, OSEnvironment{})
}

// GetComposeBaseArgsWithEnvironment is the injectable form of
// GetComposeBaseArgs.
func GetComposeBaseArgsWithEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment) ([]string, error) {
	cliArgs, err := DockerCLIArgsWithEnvironment(common, environment, "compose")
	if err != nil {
		return nil, err
	}

	if args.ProjectName != "" {
		cliArgs = append(cliArgs, "--project-name", args.ProjectName)
	}

	for _, f := range args.Files {
		cliArgs = append(cliArgs, "--file", f)
	}

	return cliArgs, nil
}

// GetComposeProjectArgsWithEnvironment returns the pinned Compose 5.4 CLI prefix
// used by event-backed Compose modules.
func GetComposeProjectArgsWithEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment) ([]string, error) {
	return GetComposeProjectArgsWithProgressEnvironment(args, common, environment, "json")
}

// GetComposeProjectArgsWithProgressEnvironment returns the common Compose
// project prefix with the requested progress format. Compose run uses plain
// progress because its stdout and stderr belong to the container command.
func GetComposeProjectArgsWithProgressEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment, progress string) ([]string, error) {
	cliArgs, err := DockerCLIArgsWithEnvironment(common, environment, "compose")
	if err != nil {
		return nil, err
	}
	cliArgs = append(cliArgs, "--ansi", "never")
	if progress != "" {
		cliArgs = append(cliArgs, "--progress", progress)
	}
	if args.ProjectSrc != "" {
		cliArgs = append(cliArgs, "--project-directory", args.ProjectSrc)
	}
	if args.ProjectName != "" {
		cliArgs = append(cliArgs, "--project-name", args.ProjectName)
	}
	for _, file := range args.Files {
		cliArgs = append(cliArgs, "--file", file)
	}
	for _, file := range args.EnvFiles {
		cliArgs = append(cliArgs, "--env-file", file)
	}
	for _, profile := range args.Profiles {
		cliArgs = append(cliArgs, "--profile", profile)
	}
	return cliArgs, nil
}

// ParseComposeJSONDocuments parses Compose `--format json` output as a JSON
// array, a single object, a 2.37+ images dictionary, or NDJSON.
func ParseComposeJSONDocuments(output []byte) ([]any, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil, nil
	}
	if output[0] == '[' {
		var documents []any
		if err := json.Unmarshal(output, &documents); err != nil {
			return nil, err
		}
		return documents, nil
	}
	if output[0] == '{' {
		var object map[string]any
		if err := json.Unmarshal(output, &object); err == nil {
			if looksLikeComposeImageMap(object) {
				result := make([]any, 0, len(object))
				for _, value := range object {
					result = append(result, value)
				}
				return result, nil
			}
			return []any{object}, nil
		}
	}
	var documents []any
	for _, rawLine := range bytes.Split(output, []byte{'\n'}) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		var document any
		if err := json.Unmarshal(line, &document); err != nil {
			return nil, fmt.Errorf("parse Compose JSON document %q: %w", line, err)
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func looksLikeComposeImageMap(object map[string]any) bool {
	if len(object) == 0 {
		return false
	}
	if _, hasRepository := object["Repository"]; hasRepository {
		return false
	}
	if _, hasID := object["ID"]; hasID {
		return false
	}
	for _, value := range object {
		entry, ok := value.(map[string]any)
		if !ok {
			return false
		}
		_, hasRepository := entry["Repository"]
		_, hasID := entry["ID"]
		if !hasRepository && !hasID {
			return false
		}
	}
	return true
}

// NormalizeComposeContainer converts a Compose `ps --format json` record into
// the upstream container dictionary (Labels map, Names/Networks lists).
func NormalizeComposeContainer(raw any) (map[string]any, error) {
	container, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Compose container document must be an object")
	}
	result := make(map[string]any, len(container))
	for key, value := range container {
		result[key] = value
	}
	result["Labels"] = normalizeComposeLabelMap(result["Labels"])
	name, _ := result["Name"].(string)
	result["Names"] = splitComposeCSV(result["Names"], name)
	networks, _ := result["Networks"].(string)
	result["Networks"] = splitComposeCSV(result["Networks"], networks)
	if result["Publishers"] == nil {
		result["Publishers"] = []any{}
	}
	return result, nil
}

func normalizeComposeLabelMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		labels := map[string]any{}
		if typed == "" {
			return labels
		}
		for _, part := range strings.Split(typed, ",") {
			key, labelValue, found := strings.Cut(part, "=")
			if !found {
				labels[key] = ""
				continue
			}
			labels[key] = labelValue
		}
		return labels
	default:
		return map[string]any{}
	}
}

func splitComposeCSV(value any, fallback string) []any {
	text, ok := value.(string)
	if !ok || text == "" {
		text = fallback
	}
	if text == "" {
		return []any{}
	}
	parts := strings.Split(text, ",")
	result := make([]any, 0, len(parts))
	for _, part := range parts {
		result = append(result, part)
	}
	return result
}

// GetComposeEnv returns the environment variables for docker compose execution.
func GetComposeEnv(args ComposeCommonArgs, common CommonArgs) ([]string, error) {
	return GetComposeEnvWithEnvironment(args, common, OSEnvironment{})
}

// GetComposeEnvWithEnvironment is the injectable form of GetComposeEnv.
func GetComposeEnvWithEnvironment(args ComposeCommonArgs, common CommonArgs, environment Environment) ([]string, error) {
	cmdEnv, err := DockerCLIEnvWithEnvironment(common, environment)
	if err != nil {
		return nil, err
	}

	// COMPOSE_PROFILES
	if len(args.Profiles) > 0 {
		cmdEnv = setEnvironment(cmdEnv, "COMPOSE_PROFILES", strings.Join(args.Profiles, ","))
	}

	// User defined env
	for k, v := range args.Env {
		cmdEnv = setEnvironment(cmdEnv, k, v)
	}

	return cmdEnv, nil
}
