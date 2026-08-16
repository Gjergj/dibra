package docker_image_build

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
)

var buildxVersionPattern = regexp.MustCompile(`(?i)\bv?(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?\b`)

type builder struct {
	request      Request
	dependencies docker.Dependencies
	environment  []string
	commandName  string
	nameTag      string
	buildx       [3]int
	outputs      []Output
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
	instance, failure := newBuilder(req, dependencies)
	if failure != nil {
		return *failure
	}

	image, exists, err := instance.inspectImage()
	if err != nil {
		return failedResponse(err.Error())
	}
	result := Response{Actions: []string{}, Image: image}
	if exists && req.Rebuild != "always" {
		return result
	}

	result.Changed = true
	if state.CheckMode {
		return result
	}

	command, secretEnvironment, err := instance.buildCommand()
	if err != nil {
		return failedResponse(err.Error())
	}
	runEnvironment := append(append([]string(nil), instance.environment...), secretEnvironment...)
	cliResult, runErr := instance.run(command, runEnvironment)
	stdout, stderr := splitCLIOutput(cliResult)
	result.Command = command
	result.Stdout = stdout
	result.Stderr = stderr
	if runErr != nil {
		result.Failed = true
		result.Msg = fmt.Sprintf("Building %s failed", instance.nameTag)
		return result
	}

	image, _, err = instance.inspectImage()
	if err != nil {
		return failedResponse(err.Error())
	}
	result.Image = image
	return result
}

func newBuilder(req Request, dependencies docker.Dependencies) (*builder, *Response) {
	if req.Name == "" {
		result := failedResponse("name is required")
		return nil, &result
	}
	if req.Path == "" {
		result := failedResponse("path is required")
		return nil, &result
	}
	if req.Tag == "" {
		req.Tag = "latest"
	}
	if req.Rebuild == "" {
		req.Rebuild = "never"
	}
	if req.Rebuild != "never" && req.Rebuild != "always" {
		result := failedResponse("rebuild must be one of never or always")
		return nil, &result
	}
	if !validTag(req.Tag) {
		result := failedResponse(fmt.Sprintf("%q is not a valid docker tag", req.Tag))
		return nil, &result
	}
	if isImageID(req.Name) || strings.Contains(req.Name, "@") {
		result := failedResponse("Image name must not be a digest")
		return nil, &result
	}
	repository, parsedTag, err := splitImageName(req.Name)
	if err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	if parsedTag != "" {
		req.Name = repository
		req.Tag = parsedTag
	}
	if !validTag(req.Tag) || strings.Contains(req.Tag, "@") {
		result := failedResponse("Image name must not contain a digest, but have a tag")
		return nil, &result
	}

	info, err := dependencies.FileSystem.Stat(req.Path)
	if err != nil || !info.IsDir() {
		result := failedResponse(fmt.Sprintf("%q is not an existing directory", req.Path))
		return nil, &result
	}
	if req.Dockerfile != "" {
		dockerfile := filepath.Join(req.Path, req.Dockerfile)
		info, err = dependencies.FileSystem.Stat(dockerfile)
		if err != nil || !info.Mode().IsRegular() {
			result := failedResponse(fmt.Sprintf("%q is not an existing file", dockerfile))
			return nil, &result
		}
	}
	if err := validateSecrets(req.Secrets, dependencies.FileSystem); err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	if err := validateOutputs(req.Outputs); err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}

	environment, err := docker.DockerCLIEnvWithEnvironment(req.CommonArgs, dependencies.Environment)
	if err != nil {
		result := failedResponse(fmt.Sprintf("invalid Docker connection options: %v", err))
		return nil, &result
	}
	commandName := req.DockerCLI
	if commandName == "" {
		commandName = "docker"
	}
	instance := &builder{
		request:      req,
		dependencies: dependencies,
		environment:  environment,
		commandName:  commandName,
		nameTag:      req.Name + ":" + req.Tag,
		outputs:      cloneOutputs(req.Outputs),
	}
	versionCommand := []string{"buildx", "version"}
	versionResult, versionErr := instance.run(versionCommand, environment)
	if versionErr != nil {
		result := failedResponse(fmt.Sprintf("Docker CLI %s does not have the buildx plugin installed", commandName))
		return nil, &result
	}
	instance.buildx, err = parseBuildxVersion(string(versionResult.Output))
	if err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	if err := instance.prepareOutputs(); err != nil {
		result := failedResponse(err.Error())
		return nil, &result
	}
	return instance, nil
}

func (instance *builder) prepareOutputs() error {
	for _, secret := range instance.request.Secrets {
		if (secret.Type == "env" || secret.Type == "value") && versionLess(instance.buildx, [3]int{0, 6, 0}) {
			return fmt.Errorf("the Docker buildx plugin has version %s, but 0.6.0 is needed for secrets of type=env and type=value", formatVersion(instance.buildx))
		}
	}
	if len(instance.outputs) > 1 && versionLess(instance.buildx, [3]int{0, 13, 0}) {
		return fmt.Errorf("the Docker buildx plugin has version %s, but 0.13.0 is needed to specify more than one output", formatVersion(instance.buildx))
	}
	if len(instance.outputs) == 0 {
		return nil
	}

	found := false
	for index := range instance.outputs {
		output := &instance.outputs[index]
		if output.Type != "image" {
			continue
		}
		if len(output.Name) == 0 {
			output.Name = StringList{instance.nameTag}
		}
		for _, name := range output.Name {
			if name == instance.nameTag {
				found = true
			}
		}
	}
	if !found {
		instance.outputs = append(instance.outputs, Output{Type: "image", Name: StringList{instance.nameTag}})
		if versionLess(instance.buildx, [3]int{0, 13, 0}) {
			return fmt.Errorf("the output does not include an image with name %s, and the Docker buildx plugin has version %s which only supports one output", instance.nameTag, formatVersion(instance.buildx))
		}
	}
	return nil
}

func (instance *builder) inspectImage() (map[string]any, bool, error) {
	command := []string{"image", "inspect", instance.nameTag, "--format", "{{json .}}"}
	result, err := instance.run(command, instance.environment)
	if err != nil {
		text := strings.ToLower(string(result.Output))
		if strings.Contains(text, "no such image") || strings.Contains(text, "no such object") {
			return map[string]any{}, false, nil
		}
		return nil, false, fmt.Errorf("failed to inspect image %s: %s", instance.nameTag, strings.TrimSpace(string(result.Output)))
	}
	var image map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(result.Output))), &image); err != nil {
		return nil, false, fmt.Errorf("decode image inspection for %s: %w", instance.nameTag, err)
	}
	return image, true, nil
}

func (instance *builder) buildCommand() ([]string, []string, error) {
	command := []string{"buildx", "build", "--progress", "plain"}
	if len(instance.outputs) == 0 {
		command = append(command, "--tag", instance.nameTag)
	}
	if instance.request.Dockerfile != "" {
		command = append(command, "--file", filepath.Join(instance.request.Path, instance.request.Dockerfile))
	}
	for _, cache := range instance.request.CacheFrom {
		command = append(command, "--cache-from", cache)
	}
	if instance.request.Pull {
		command = append(command, "--pull")
	}
	if instance.request.Network != "" {
		command = append(command, "--network", instance.request.Network)
	}
	if instance.request.NoCache {
		command = append(command, "--no-cache")
	}
	appendSortedMapArguments(&command, "--add-host", instance.request.EtcHosts, ":")
	appendSortedMapArguments(&command, "--build-arg", instance.request.Args, "=")
	if instance.request.Target != "" {
		command = append(command, "--target", instance.request.Target)
	}
	for _, platform := range instance.request.Platform {
		command = append(command, "--platform", platform)
	}
	if instance.request.ShmSize != "" {
		bytes, err := byteValue(instance.request.ShmSize)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert shm_size to bytes: %w", err)
		}
		command = append(command, "--shm-size", strconv.FormatInt(bytes, 10))
	}
	appendSortedMapArguments(&command, "--label", instance.request.Labels, "=")

	secretEnvironment := []string{}
	randomPrefix := ""
	for index, secret := range instance.request.Secrets {
		switch secret.Type {
		case "file":
			command = append(command, "--secret", fmt.Sprintf("id=%s,type=file,src=%s", secret.ID, secret.Src))
		case "env":
			command = append(command, "--secret", fmt.Sprintf("id=%s,type=env,env=%s", secret.ID, secret.Env))
		case "value":
			if randomPrefix == "" {
				var entropy [16]byte
				if _, err := rand.Read(entropy[:]); err != nil {
					return nil, nil, fmt.Errorf("generate secret environment name: %w", err)
				}
				randomPrefix = strings.TrimRight(base64.StdEncoding.EncodeToString(entropy[:]), "=")
			}
			name := fmt.Sprintf("ANSIBLE_DOCKER_COMPOSE_ENV_SECRET_%s_%d", randomPrefix, index)
			secretEnvironment = append(secretEnvironment, name+"="+secret.Value)
			command = append(command, "--secret", fmt.Sprintf("id=%s,type=env,env=%s", secret.ID, name))
		}
	}
	for _, output := range instance.outputs {
		parts := outputParts(output)
		command = append(command, "--output", quoteCSV(parts))
	}
	command = append(command, "--", instance.request.Path)
	return command, secretEnvironment, nil
}

func (instance *builder) run(command, environment []string) (docker.CLIResult, error) {
	args, err := docker.DockerCLIArgsWithEnvironment(instance.request.CommonArgs, instance.dependencies.Environment, command...)
	if err != nil {
		return docker.CLIResult{}, err
	}
	return instance.dependencies.CLIRunner.Run(context.Background(), docker.CLICommand{
		Name: instance.commandName,
		Args: args,
		Env:  environment,
	})
}

func validateSecrets(secrets []Secret, fileSystem docker.FileSystem) error {
	for index, secret := range secrets {
		if secret.ID == "" {
			return fmt.Errorf("secrets[%d].id is required", index)
		}
		switch secret.Type {
		case "file":
			if secret.Src == "" {
				return fmt.Errorf("secrets[%d].src is required for type=file", index)
			}
			if secret.Env != "" || secret.Value != "" {
				return fmt.Errorf("secrets[%d] src, env, and value are mutually exclusive", index)
			}
			info, err := fileSystem.Stat(secret.Src)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("%q is not an existing file", secret.Src)
			}
		case "env":
			if secret.Env == "" {
				return fmt.Errorf("secrets[%d].env is required for type=env", index)
			}
			if secret.Src != "" || secret.Value != "" {
				return fmt.Errorf("secrets[%d] src, env, and value are mutually exclusive", index)
			}
		case "value":
			if secret.Value == "" {
				return fmt.Errorf("secrets[%d].value is required for type=value", index)
			}
			if secret.Src != "" || secret.Env != "" {
				return fmt.Errorf("secrets[%d] src, env, and value are mutually exclusive", index)
			}
		default:
			return fmt.Errorf("secrets[%d].type must be one of file, env, or value", index)
		}
	}
	return nil
}

func validateOutputs(outputs []Output) error {
	for index, output := range outputs {
		switch output.Type {
		case "local", "tar", "oci":
			if output.Dest == "" {
				return fmt.Errorf("outputs[%d].dest is required for type=%s", index, output.Type)
			}
		case "docker", "image":
		default:
			return fmt.Errorf("outputs[%d].type must be one of local, tar, oci, docker, or image", index)
		}
		if output.Dest != "" && (len(output.Name) > 0 || output.Push) {
			return fmt.Errorf("outputs[%d] dest is mutually exclusive with name and push", index)
		}
		if output.Context != "" && (len(output.Name) > 0 || output.Push) {
			return fmt.Errorf("outputs[%d] context is mutually exclusive with name and push", index)
		}
	}
	return nil
}

func appendSortedMapArguments(command *[]string, option string, values OptionMap, separator string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		*command = append(*command, option, key+separator+dockerAPIOptionText(values[key], false))
	}
}

func dockerAPIOptionText(value any, nested bool) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		if nested {
			return pythonOptionStringRepr(typed)
		}
		return typed
	case bool:
		if nested {
			if typed {
				return "True"
			}
			return "False"
		}
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	case float64:
		text := strconv.FormatFloat(typed, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		return text
	case []any:
		items := make([]string, len(typed))
		for index, item := range typed {
			items[index] = dockerAPIOptionText(item, true)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		return pythonOptionMapText(typed)
	case OptionMap:
		return pythonOptionMapText(map[string]any(typed))
	default:
		return fmt.Sprint(typed)
	}
}

func pythonOptionMapText(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, pythonOptionStringRepr(key)+": "+dockerAPIOptionText(values[key], true))
	}
	return "{" + strings.Join(items, ", ") + "}"
}

func pythonOptionStringRepr(value string) string {
	quote := byte('\'')
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = '"'
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	escaped = strings.ReplaceAll(escaped, "\r", `\r`)
	escaped = strings.ReplaceAll(escaped, "\t", `\t`)
	escaped = strings.ReplaceAll(escaped, string(quote), `\`+string(quote))
	return string(quote) + escaped + string(quote)
}

func outputParts(output Output) []string {
	parts := []string{"type=" + output.Type}
	switch output.Type {
	case "local", "tar", "oci":
		parts = append(parts, "dest="+output.Dest)
	case "docker":
		if output.Dest != "" {
			parts = append(parts, "dest="+output.Dest)
		}
		if output.Context != "" {
			parts = append(parts, "context="+output.Context)
		}
	case "image":
		if len(output.Name) > 0 {
			parts = append(parts, "name="+strings.Join(output.Name, ","))
		}
		if output.Push {
			parts = append(parts, "push=true")
		}
	}
	return parts
}

func quoteCSV(parts []string) string {
	result := make([]string, len(parts))
	for index, value := range parts {
		if strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\",\r\n") {
			result[index] = value
		} else {
			result[index] = `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
		}
	}
	return strings.Join(result, ",")
}

func splitCLIOutput(result docker.CLIResult) (string, string) {
	if result.Stdout == nil && result.Stderr == nil {
		return string(result.Output), ""
	}
	return string(result.Stdout), string(result.Stderr)
}

func cloneOutputs(outputs []Output) []Output {
	result := make([]Output, len(outputs))
	copy(result, outputs)
	for index := range result {
		result[index].Name = append(StringList(nil), result[index].Name...)
	}
	return result
}

func splitImageName(value string) (string, string, error) {
	reference := docker.ParseImageReference(value)
	if err := reference.Validate(); err != nil {
		return "", "", fmt.Errorf("invalid image name %q: %w", value, err)
	}
	if reference.Digest != "" {
		return "", "", fmt.Errorf("Image name must not be a digest")
	}
	tag := reference.Tag
	reference.Tag = ""
	if tag != "" {
		return reference.String(), tag, nil
	}
	return reference.String(), "", nil
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
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-' {
			continue
		}
		if index == 0 {
			return false
		}
		return false
	}
	first := value[0]
	return first != '.' && first != '-'
}

func parseBuildxVersion(output string) ([3]int, error) {
	match := buildxVersionPattern.FindStringSubmatch(output)
	if len(match) != 4 {
		return [3]int{}, fmt.Errorf("cannot determine Docker buildx plugin version from %q", strings.TrimSpace(output))
	}
	var version [3]int
	for index := range version {
		version[index], _ = strconv.Atoi(match[index+1])
	}
	return version, nil
}

func versionLess(left, right [3]int) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func formatVersion(version [3]int) string {
	return fmt.Sprintf("%d.%d.%d", version[0], version[1], version[2])
}

func byteValue(value string) (int64, error) {
	trimmed := strings.TrimSpace(strings.ToUpper(value))
	if trimmed == "" {
		return 0, fmt.Errorf("value is empty")
	}
	index := 0
	for index < len(trimmed) && trimmed[index] >= '0' && trimmed[index] <= '9' {
		index++
	}
	if index == 0 {
		return 0, fmt.Errorf("invalid size %q", value)
	}
	number, err := strconv.ParseInt(trimmed[:index], 10, 64)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("invalid size %q", value)
	}
	unit := strings.TrimSuffix(strings.TrimSpace(trimmed[index:]), "B")
	power := 0
	switch unit {
	case "":
	case "K":
		power = 1
	case "M":
		power = 2
	case "G":
		power = 3
	case "T":
		power = 4
	case "P":
		power = 5
	default:
		return 0, fmt.Errorf("invalid size unit %q", unit)
	}
	for range power {
		if number > (1<<63-1)/1024 {
			return 0, fmt.Errorf("size %q overflows int64", value)
		}
		number *= 1024
	}
	return number, nil
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message, Actions: []string{}, Image: map[string]any{}}
}
