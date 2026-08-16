package docker_image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var imageIDPattern = regexp.MustCompile(`^(?:sha256:)?[0-9a-fA-F]{12,64}$`)

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
	result := emptyResponse()
	req = applyDefaults(req)
	if err := validateRequest(req); err != nil {
		return failedResponse(err.Error())
	}

	dependencies = dependencies.Resolve()
	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	name, tag, reference, err := resolveRequestedReference(req.Name, req.Tag)
	if err != nil {
		return failedResponse(err.Error())
	}

	if req.State == "absent" {
		return removeImage(ctx, cli, reference, req.ForceAbsent, state.CheckMode)
	}

	_, existingRaw, existingFound, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if existingFound {
		result.Image = existingRaw
	}

	forceSource := req.ForceSource
	if req.Pull != nil && req.Pull.Policy == "always" {
		forceSource = true
	}
	if req.Pull != nil && req.Pull.Policy == "never" && !existingFound {
		return failedResponse(fmt.Sprintf("Cannot find the image %s locally.", reference))
	}

	if !existingFound || forceSource {
		switch req.Source {
		case "pull":
			if isImageID(req.Name) {
				return failedResponse(fmt.Sprintf("Image name must not be an image ID for source=pull; got: %s", req.Name))
			}
			result.Actions = append(result.Actions, fmt.Sprintf("Pulled image %s", reference))
			result.Changed = true
			if !state.CheckMode {
				pulledRaw, pullErr := pullImage(ctx, cli, reference, req, dependencies)
				if pullErr != nil {
					return failedResponse(pullErr.Error())
				}
				result.Image = pulledRaw
				if existingFound && imageID(existingRaw) == imageID(pulledRaw) {
					result.Changed = false
				}
			}
		case "build":
			if isImageID(req.Name) {
				return failedResponse(fmt.Sprintf("Image name must not be an image ID for source=build; got: %s", req.Name))
			}
			info, statErr := dependencies.FileSystem.Stat(req.Build.Path)
			if statErr != nil || !info.IsDir() {
				return failedResponse(fmt.Sprintf("Requested build path %s could not be found or you do not have access.", req.Build.Path))
			}
			action := fmt.Sprintf("Built image %s from %s", reference, req.Build.Path)
			result.Actions = append(result.Actions, action)
			result.Changed = true
			if !state.CheckMode {
				buildRaw, stdout, buildErr := buildImage(ctx, cli, reference, req, dependencies)
				if buildErr != nil {
					return failedResponse(buildErr.Error())
				}
				result.Image = buildRaw
				result.Stdout = stdout
				if existingFound && imageID(existingRaw) == imageID(buildRaw) {
					result.Changed = false
				}
			}
		case "load":
			info, statErr := dependencies.FileSystem.Stat(req.LoadPath)
			if statErr != nil || !info.Mode().IsRegular() {
				return failedResponse(fmt.Sprintf("Error loading image %s. Specified path %s does not exist.", req.Name, req.LoadPath))
			}
			result.Actions = append(result.Actions, fmt.Sprintf("Loaded image %s from %s", reference, req.LoadPath))
			result.Changed = true
			if !state.CheckMode {
				loadedRaw, stdout, loadErr := loadImage(ctx, cli, reference, req.Name, req.LoadPath, dependencies)
				if loadErr != nil {
					response := failedResponse(loadErr.Error())
					response.Stdout = stdout
					return response
				}
				result.Image = loadedRaw
				if existingFound && imageID(existingRaw) == imageID(loadedRaw) {
					result.Changed = false
				}
			}
		case "local":
			if !existingFound {
				return failedResponse(fmt.Sprintf("Cannot find the image %s locally.", reference))
			}
		}
	}

	if req.ArchivePath != "" {
		archiveChanged, archiveAction, archiveErr := archiveImage(ctx, cli, reference, result.Image, req.ArchivePath, dependencies, state.CheckMode)
		if archiveErr != nil {
			return failedResponse(archiveErr.Error())
		}
		if archiveAction != "" {
			result.Actions = append(result.Actions, archiveAction)
		}
		result.Changed = archiveChanged
	}

	if req.Repository != "" {
		target, targetErr := repositoryReference(req.Repository, tag)
		if targetErr != nil {
			return failedResponse(targetErr.Error())
		}
		tagChanged, tagAction, taggedRaw, tagErr := tagImage(ctx, cli, reference, target, req.ForceTag, state.CheckMode)
		if tagErr != nil {
			return failedResponse(tagErr.Error())
		}
		if tagAction != "" {
			result.Actions = append(result.Actions, tagAction)
		}
		result.Changed = result.Changed || tagChanged
		if taggedRaw != nil {
			result.Image = taggedRaw
		}
		if req.Push {
			pushChanged, pushAction, pushedRaw, pushErr := pushImage(ctx, cli, target, name, req, dependencies, state.CheckMode)
			if pushErr != nil {
				return failedResponse(pushErr.Error())
			}
			if pushAction != "" {
				result.Actions = append(result.Actions, pushAction)
			}
			result.Changed = pushChanged
			if pushedRaw != nil {
				result.Image = pushedRaw
			}
		}
	} else if req.Push {
		if isImageID(req.Name) {
			return failedResponse(fmt.Sprintf("Cannot push an image ID: %s", req.Name))
		}
		pushChanged, pushAction, pushedRaw, pushErr := pushImage(ctx, cli, reference, name, req, dependencies, state.CheckMode)
		if pushErr != nil {
			return failedResponse(pushErr.Error())
		}
		if pushAction != "" {
			result.Actions = append(result.Actions, pushAction)
		}
		result.Changed = pushChanged
		if pushedRaw != nil {
			result.Image = pushedRaw
		}
	}

	return result
}

func emptyResponse() Response {
	return Response{Actions: []string{}, Image: map[string]any{}}
}

func failedResponse(message string) Response {
	response := emptyResponse()
	response.Failed = true
	response.Msg = message
	return response
}

func applyDefaults(req Request) Request {
	if req.State == "" {
		req.State = "present"
	}
	if req.Tag == "" && (req.providedArguments == nil || !req.argumentProvided("tag")) {
		req.Tag = "latest"
	}
	return req
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.State != "present" && req.State != "absent" {
		return fmt.Errorf("state must be present or absent")
	}
	if req.Tag != "" && !validTag(req.Tag) {
		return fmt.Errorf("%q is not a valid docker tag", req.Tag)
	}
	if req.Repository != "" && isImageID(req.Repository) {
		return fmt.Errorf("`repository` must not be an image ID; got: %s", req.Repository)
	}
	if req.State == "absent" {
		return nil
	}
	switch req.Source {
	case "build", "load", "pull", "local":
	case "":
		return fmt.Errorf("source is required when state=present")
	default:
		return fmt.Errorf("source must be build, load, pull, or local")
	}
	if req.Source == "build" {
		if req.Build == nil || strings.TrimSpace(req.Build.Path) == "" {
			return fmt.Errorf(`If "source" is set to "build", the "build.path" option must be specified.`)
		}
		if req.Build.HTTPTimeout < 0 {
			return fmt.Errorf("build.http_timeout must be positive")
		}
	}
	if req.Source == "load" && strings.TrimSpace(req.LoadPath) == "" {
		return fmt.Errorf("load_path is required when source=load")
	}
	if req.Push && req.Repository == "" && isImageID(req.Name) {
		return fmt.Errorf("Cannot push an image by ID; specify `repository` to tag and push the image with ID %s instead", req.Name)
	}
	return nil
}

func resolveRequestedReference(value, defaultTag string) (string, string, string, error) {
	if isImageID(value) {
		return value, defaultTag, value, nil
	}
	parsed := docker.ParseImageReference(value)
	if err := parsed.Validate(); err != nil {
		return "", "", "", fmt.Errorf("invalid image name %q: %v", value, err)
	}
	name := value
	tag := defaultTag
	if parsed.Tag != "" {
		tag = parsed.Tag
		parsed.Tag = ""
		name = parsed.String()
	}
	reference, err := docker.JoinImageNameTag(name, tag)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid image name %q: %v", value, err)
	}
	return name, tag, reference, nil
}

func repositoryReference(repository, tag string) (string, error) {
	if isImageID(repository) {
		return "", fmt.Errorf("`repository` must not be an image ID; got: %s", repository)
	}
	reference, err := docker.JoinImageNameTag(repository, tag)
	if err != nil {
		return "", fmt.Errorf("invalid repository %q: %v", repository, err)
	}
	return reference, nil
}

func isImageID(value string) bool {
	return imageIDPattern.MatchString(value)
}

func validTag(value string) bool {
	if value == "" || len(value) > 128 {
		return value == ""
	}
	first := value[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9') || first == '_') {
		return false
	}
	for _, character := range value[1:] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '.' || character == '-') {
			return false
		}
	}
	return true
}

func inspectImage(ctx context.Context, cli client.APIClient, reference string) (client.ImageInspectResult, map[string]any, bool, error) {
	inspect, err := cli.ImageInspect(ctx, reference)
	if docker.IsNotFoundError(err) {
		return client.ImageInspectResult{}, nil, false, nil
	}
	if err != nil {
		return client.ImageInspectResult{}, nil, false, err
	}
	raw, err := inspectMap(inspect)
	if err != nil {
		return client.ImageInspectResult{}, nil, false, err
	}
	return inspect, raw, true, nil
}

func inspectMap(inspect client.ImageInspectResult) (map[string]any, error) {
	encoded, err := json.Marshal(inspect)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func imageID(inspect map[string]any) string {
	if inspect == nil {
		return ""
	}
	value, _ := inspect["Id"].(string)
	return value
}

func removeImage(ctx context.Context, cli client.APIClient, reference string, force, checkMode bool) Response {
	_, raw, found, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return failedResponse(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	if !found {
		return emptyResponse()
	}
	result := emptyResponse()
	result.Changed = true
	result.Actions = append(result.Actions, fmt.Sprintf("Removed image %s", reference))
	result.Image = raw
	result.Image["state"] = "Deleted"
	if checkMode {
		return result
	}
	if _, err := cli.ImageRemove(ctx, reference, client.ImageRemoveOptions{Force: force, PruneChildren: false}); err != nil {
		if docker.IsNotFoundError(err) {
			return result
		}
		return failedResponse(fmt.Sprintf("Error removing image %s - %v", reference, err))
	}
	return result
}

func pullImage(ctx context.Context, cli client.APIClient, reference string, req Request, dependencies docker.Dependencies) (map[string]any, error) {
	auth, err := registryAuth(ctx, reference, req, dependencies, false)
	if err != nil {
		return nil, err
	}
	options := client.ImagePullOptions{RegistryAuth: auth}
	if req.Pull != nil && req.Pull.Platform != "" {
		platform, parseErr := parsePlatform(req.Pull.Platform)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid pull.platform %q: %v", req.Pull.Platform, parseErr)
		}
		options.Platforms = append(options.Platforms, platform)
	}
	stream, err := cli.ImagePull(ctx, reference, options)
	if err != nil {
		return nil, fmt.Errorf("Error pulling image %s - %v", reference, err)
	}
	defer stream.Close()
	parsed := docker.ParsePullPushStream(stream)
	if parsed.Error != nil {
		return nil, fmt.Errorf("Error pulling image %s - %v", reference, parsed.Error)
	}
	_, raw, found, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return nil, fmt.Errorf("Error inspecting image %s after pull - %v", reference, err)
	}
	if !found {
		return nil, fmt.Errorf("Error pulling image %s - image was not present after pull", reference)
	}
	return raw, nil
}

func buildImage(ctx context.Context, cli client.APIClient, reference string, req Request, dependencies docker.Dependencies) (map[string]any, string, error) {
	build := req.Build
	info, err := dependencies.FileSystem.Stat(build.Path)
	if err != nil || !info.IsDir() {
		return nil, "", fmt.Errorf("Requested build path %s could not be found or you do not have access.", build.Path)
	}
	contextReader, err := buildContextArchive(build.Path, dependencies.FileSystem)
	if err != nil {
		return nil, "", fmt.Errorf("Error preparing build context %s - %v", build.Path, err)
	}
	defer contextReader.Close()

	options := client.ImageBuildOptions{
		Tags:        []string{reference},
		Context:     contextReader,
		NoCache:     build.NoCache,
		Remove:      build.Remove == nil || *build.Remove,
		ForceRemove: build.Remove == nil || *build.Remove,
		PullParent:  build.Pull != nil && *build.Pull,
		CPUSetCPUs:  containerLimitCPUSet(build.ContainerLimits),
		CPUShares:   containerLimitCPUShares(build.ContainerLimits),
		NetworkMode: build.Network,
		Dockerfile:  build.Dockerfile,
		BuildArgs:   stringifyPointerMap(build.Args),
		Labels:      stringifyMap(build.Labels),
		CacheFrom:   build.CacheFrom,
		ExtraHosts:  extraHosts(build.EtcHosts),
		Target:      build.Target,
	}
	options.AuthConfigs, err = dockerConfigAuthConfigs(ctx, req, reference, dependencies)
	if err != nil {
		return nil, "", fmt.Errorf("Error resolving registry authentication for build - %v", err)
	}
	if build.Platform != "" {
		platform, parseErr := parsePlatform(build.Platform)
		if parseErr != nil {
			return nil, "", fmt.Errorf("invalid build.platform %q: %v", build.Platform, parseErr)
		}
		options.Platforms = append(options.Platforms, platform)
	}
	if build.ContainerLimits != nil {
		options.Memory, err = byteValue(build.ContainerLimits.Memory, false)
		if err != nil {
			return nil, "", fmt.Errorf("Failed to convert build.container_limits.memory to bytes: %v", err)
		}
		options.MemorySwap, err = byteValue(build.ContainerLimits.MemorySwap, true)
		if err != nil {
			return nil, "", fmt.Errorf("Failed to convert build.container_limits.memswap to bytes: %v", err)
		}
	}
	options.ShmSize, err = byteValue(build.ShmSize, false)
	if err != nil {
		return nil, "", fmt.Errorf("Failed to convert build.shm_size to bytes: %v", err)
	}
	if build.UseConfigProxy != nil && *build.UseConfigProxy {
		for key, value := range dockerConfigProxyArgs(req, dependencies) {
			if _, exists := options.BuildArgs[key]; !exists {
				copy := value
				options.BuildArgs[key] = &copy
			}
		}
	}

	buildCtx := ctx
	var cancel context.CancelFunc
	if build.HTTPTimeout > 0 {
		buildCtx, cancel = context.WithTimeout(ctx, time.Duration(build.HTTPTimeout)*time.Second)
		defer cancel()
	}
	response, err := cli.ImageBuild(buildCtx, contextReader, options)
	if err != nil {
		return nil, "", fmt.Errorf("Error building %s - %v", req.Name, err)
	}
	defer response.Body.Close()
	parsed := docker.ParseBuildStream(response.Body)
	stdout := strings.Join(parsed.Logs, "\n")
	if parsed.Error != nil {
		return nil, stdout, fmt.Errorf("Error building %s - %v, logs: %v", req.Name, parsed.Error, parsed.Logs)
	}
	_, raw, found, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return nil, stdout, err
	}
	if !found {
		return nil, stdout, fmt.Errorf("Error building %s - resulting image was not found", req.Name)
	}
	return raw, stdout, nil
}

func loadImage(ctx context.Context, cli client.APIClient, reference, requestedName, path string, dependencies docker.Dependencies) (map[string]any, string, error) {
	archive, err := dependencies.FileSystem.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("Error opening image %s - %v", path, err)
	}
	defer archive.Close()
	response, err := cli.ImageLoad(ctx, archive)
	if err != nil {
		return nil, "", fmt.Errorf("Error loading image %s - %v", requestedName, err)
	}
	defer response.Close()
	parsed := docker.ParseLoadStream(response)
	stdout := strings.Join(parsed.Logs, "\n")
	if parsed.Error != nil {
		return nil, stdout, fmt.Errorf("Error loading image %s - %v", requestedName, parsed.Error)
	}
	if len(parsed.Images) == 0 {
		return nil, stdout, fmt.Errorf("Detected no loaded images. Archive potentially corrupt?")
	}
	expected := reference
	matched := false
	for _, loaded := range parsed.Images {
		if loaded == expected || (isImageID(requestedName) && strings.EqualFold(loaded, requestedName)) {
			matched = true
			break
		}
	}
	if !matched {
		found := append([]string(nil), parsed.Images...)
		sort.Strings(found)
		quoted := make([]string, len(found))
		for index, value := range found {
			quoted[index] = fmt.Sprintf("%q", value)
		}
		return nil, stdout, fmt.Errorf("The archive did not contain image '%s'. Instead, found %s.", expected, strings.Join(quoted, ", "))
	}
	_, raw, found, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return nil, stdout, err
	}
	if !found {
		return nil, stdout, fmt.Errorf("The archive did not contain image '%s'.", expected)
	}
	return raw, stdout, nil
}

func archiveImage(ctx context.Context, cli client.APIClient, reference string, inspect map[string]any, path string, dependencies docker.Dependencies, checkMode bool) (bool, string, error) {
	if imageID(inspect) == "" {
		return false, "", nil
	}
	action := fmt.Sprintf("Archived image %s to %s, since none present", reference, path)
	if existing, err := dependencies.FileSystem.Open(path); err == nil {
		manifest, manifestErr := docker.ReadImageArchiveManifest(existing)
		_ = existing.Close()
		if manifestErr == nil && docker.ImageArchiveMatches(manifest, map[string]string{reference: imageID(inspect)}) {
			return false, "", nil
		}
		if manifestErr != nil {
			action = fmt.Sprintf("Archived image %s to %s, overwriting an unreadable archive file", reference, path)
		} else if len(manifest) > 0 {
			names := strings.Join(manifest[0].RepoTags, ", ")
			action = fmt.Sprintf("Archived image %s to %s, overwriting archive with image %s named %s", reference, path, manifest[0].ImageID, names)
		}
	}
	if checkMode {
		return true, action, nil
	}
	stream, err := cli.ImageSave(ctx, []string{reference})
	if err != nil {
		return false, "", fmt.Errorf("Error getting image %s - %v", reference, err)
	}
	defer stream.Close()
	output, err := dependencies.FileSystem.Create(path)
	if err != nil {
		return false, "", fmt.Errorf("Error writing image archive %s - %v", path, err)
	}
	if _, err := io.Copy(output, stream); err != nil {
		_ = output.Close()
		return false, "", fmt.Errorf("Error writing image archive %s - %v", path, err)
	}
	if err := output.Close(); err != nil {
		return false, "", fmt.Errorf("Error writing image archive %s - %v", path, err)
	}
	return true, action, nil
}

func tagImage(ctx context.Context, cli client.APIClient, source, target string, force, checkMode bool) (bool, string, map[string]any, error) {
	_, existing, found, err := inspectImage(ctx, cli, target)
	if err != nil {
		return false, "", nil, err
	}
	if found && !force {
		return false, "", existing, nil
	}
	action := fmt.Sprintf("Tagged image %s to %s", source, target)
	if checkMode {
		return true, action, existing, nil
	}
	if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target}); err != nil {
		return false, "", nil, fmt.Errorf("Error: failed to tag image - %v", err)
	}
	_, tagged, taggedFound, err := inspectImage(ctx, cli, target)
	if err != nil {
		return false, "", nil, err
	}
	if !taggedFound {
		return false, "", nil, fmt.Errorf("Error: failed to tag image - target image was not found")
	}
	if found && imageID(existing) == imageID(tagged) {
		return false, action, tagged, nil
	}
	return true, action, tagged, nil
}

func pushImage(ctx context.Context, cli client.APIClient, reference, actionName string, req Request, dependencies docker.Dependencies, checkMode bool) (bool, string, map[string]any, error) {
	action := fmt.Sprintf("Pushed image %s to %s", actionName, reference)
	if checkMode {
		return true, action, nil, nil
	}
	auth, err := registryAuth(ctx, reference, req, dependencies, true)
	if err != nil {
		return false, "", nil, err
	}
	stream, err := cli.ImagePush(ctx, reference, client.ImagePushOptions{RegistryAuth: auth})
	if err != nil {
		return false, "", nil, fmt.Errorf("Error pushing image %s: %v", reference, err)
	}
	defer stream.Close()
	parsed := docker.ParsePullPushStream(stream)
	if parsed.Error != nil {
		return false, "", nil, fmt.Errorf("Error pushing image %s: %v", reference, parsed.Error)
	}
	changed := false
	for _, line := range parsed.Logs {
		if strings.HasSuffix(line, ": Pushing") || strings.HasSuffix(line, ": Pushed") || line == "Pushing" || line == "Pushed" {
			changed = true
		}
	}
	_, raw, found, err := inspectImage(ctx, cli, reference)
	if err != nil {
		return false, "", nil, err
	}
	if !found {
		raw = map[string]any{}
	}
	if len(parsed.Logs) > 0 {
		raw["push_status"] = parsed.Logs[len(parsed.Logs)-1]
	}
	return changed, action, raw, nil
}

func dockerConfigAuthConfigs(ctx context.Context, req Request, reference string, dependencies docker.Dependencies) (map[string]registry.AuthConfig, error) {
	result := map[string]registry.AuthConfig{}
	data, found, err := dockerConfigBytes(dependencies)
	if err != nil {
		return nil, err
	}
	if found {
		result, err = docker.AllRegistryAuthConfigs(ctx, data, dependencies)
		if err != nil {
			return nil, err
		}
	}
	if req.RegistryUsername != "" || req.RegistryPassword != "" {
		server, registryErr := docker.RegistryName(reference)
		if registryErr != nil {
			return nil, registryErr
		}
		result[server] = registry.AuthConfig{
			Username:      req.RegistryUsername,
			Password:      req.RegistryPassword,
			ServerAddress: server,
		}
	}
	return result, nil
}

func registryAuth(ctx context.Context, reference string, req Request, dependencies docker.Dependencies, requireHeader bool) (string, error) {
	if req.RegistryUsername != "" || req.RegistryPassword != "" {
		return docker.EncodeRegistryAuthForImage(reference, req.RegistryUsername, req.RegistryPassword)
	}
	return docker.ResolveRegistryAuthForImageContext(ctx, reference, dependencies, requireHeader)
}

func dockerConfigBytes(dependencies docker.Dependencies) ([]byte, bool, error) {
	var directory string
	if configured, found := dependencies.Environment.LookupEnv("DOCKER_CONFIG"); found && configured != "" {
		directory = configured
	} else {
		home, err := dependencies.FileSystem.UserHomeDir()
		if err != nil {
			return nil, false, nil
		}
		directory = filepath.Join(home, ".docker")
	}
	data, err := dependencies.FileSystem.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		return nil, false, nil
	}
	return data, true, nil
}

func dockerConfigProxyArgs(req Request, dependencies docker.Dependencies) map[string]string {
	data, found, _ := dockerConfigBytes(dependencies)
	if !found {
		return nil
	}
	var config struct {
		Proxies map[string]map[string]string `json:"proxies"`
	}
	if json.Unmarshal(data, &config) != nil {
		return nil
	}
	values := map[string]string(nil)
	if connection, err := docker.ResolveConnectionWithEnvironment(req.CommonArgs, dependencies.Environment); err == nil {
		values = config.Proxies[connection.DockerHost]
	}
	if values == nil {
		values = config.Proxies["default"]
	}
	mapping := map[string]string{
		"httpProxy":  "HTTP_PROXY",
		"httpsProxy": "HTTPS_PROXY",
		"noProxy":    "NO_PROXY",
		"ftpProxy":   "FTP_PROXY",
	}
	result := map[string]string{}
	for source, target := range mapping {
		if value := values[source]; value != "" {
			result[target] = value
			result[strings.ToLower(target)] = value
		}
	}
	return result
}

func stringifyMap(values map[string]any) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = fmt.Sprint(value)
	}
	return result
}

func stringifyPointerMap(values map[string]any) map[string]*string {
	if values == nil {
		return nil
	}
	result := make(map[string]*string, len(values))
	for key, value := range values {
		text := fmt.Sprint(value)
		result[key] = &text
	}
	return result
}

func extraHosts(values map[string]any) []string {
	if values == nil {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, fmt.Sprintf("%s:%v", key, values[key]))
	}
	return result
}

func containerLimitCPUSet(limits *ContainerLimits) string {
	if limits == nil {
		return ""
	}
	return limits.CPUSetCPUs
}

func containerLimitCPUShares(limits *ContainerLimits) int64 {
	if limits == nil {
		return 0
	}
	return limits.CPUShares
}

func byteValue(value string, allowUnlimited bool) (int64, error) {
	if value == "" {
		return 0, nil
	}
	if allowUnlimited && (value == "unlimited" || value == "-1") {
		return -1, nil
	}
	return units.RAMInBytes(value)
}

func parsePlatform(value string) (ocispec.Platform, error) {
	parts := strings.Split(value, "/")
	if len(parts) < 1 || len(parts) > 3 || parts[0] == "" {
		return ocispec.Platform{}, fmt.Errorf("must use os[/arch[/variant]]")
	}
	result := ocispec.Platform{OS: strings.ToLower(parts[0])}
	if len(parts) >= 2 {
		result.Architecture = strings.ToLower(parts[1])
	}
	if len(parts) == 3 {
		result.Variant = strings.ToLower(parts[2])
	}
	return result, nil
}
