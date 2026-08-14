package docker_swarm_service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

const updateRetries = 2

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
	if strings.TrimSpace(req.Name) == "" {
		return failedResponse("missing required arguments: name")
	}
	stateName := req.State
	if stateName == "" {
		stateName = "present"
	}
	switch stateName {
	case "present", "absent":
	default:
		return failedResponse(fmt.Sprintf("state must be present or absent, got %q", stateName))
	}
	req.State = stateName

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	manager := &serviceManager{
		req:          req,
		checkMode:    state.CheckMode,
		diffMode:     state.DiffMode,
		cli:          cli,
		ctx:          ctx,
		clock:        dependencies.Clock,
		fs:           dependencies.FileSystem,
		retries:      updateRetries,
		resolveImage: resolveBool(req.ResolveImage),
	}
	return manager.runSafe()
}

type serviceManager struct {
	req           Request
	checkMode     bool
	diffMode      bool
	cli           client.APIClient
	ctx           context.Context
	clock         docker.Clock
	fs            docker.FileSystem
	retries       int
	resolveImage  bool
	networkNames  map[string]string
}

func (manager *serviceManager) runSafe() Response {
	for {
		response := manager.run()
		if !response.Failed || manager.retries <= 0 || !strings.Contains(response.Msg, "update out of sequence") {
			return response
		}
		manager.retries--
		manager.clock.Sleep(time.Second)
	}
}

func (manager *serviceManager) run() Response {
	image, err := manager.imageDigest(manager.req.Image)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error looking for an image named %s: %v", manager.req.Image, err))
	}

	existing, found, err := manager.inspect()
	if err != nil {
		return failedResponse(fmt.Sprintf("Error looking for service named %s: %v", manager.req.Name, err))
	}

	var current *serviceState
	if found {
		current, err = serviceFromInspect(existing)
		if err != nil {
			return failedResponse(err.Error())
		}
	}

	secretIDs, err := manager.secretIDs()
	if err != nil {
		return failedResponse(err.Error())
	}
	configIDs, err := manager.configIDs()
	if err != nil {
		return failedResponse(err.Error())
	}
	networkIDs, err := manager.networkIDs()
	if err != nil {
		return failedResponse(err.Error())
	}

	forceUpdate := uint64(0)
	if manager.req.ForceUpdate {
		forceUpdate = uint64(manager.clock.Now().UnixNano())
	}
	desired, err := desiredService(manager.req, image, current, secretIDs, configIDs, networkIDs, forceUpdate, manager.fs.ReadFile)
	if err != nil {
		return failedResponse(fmt.Sprintf("Error parsing module parameters: %v", err))
	}

	if current != nil {
		if manager.req.State == "absent" {
			return manager.remove(current)
		}
		return manager.update(existing, current, desired)
	}
	if manager.req.State == "absent" {
		return Response{Msg: "Service absent", Changes: []string{}}
	}
	if strings.TrimSpace(manager.req.Image) == "" {
		return failedResponse("image is required when creating a service")
	}
	return manager.create(desired)
}

func (manager *serviceManager) inspect() (swarm.Service, bool, error) {
	result, err := manager.cli.ServiceInspect(manager.ctx, manager.req.Name, client.ServiceInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return swarm.Service{}, false, nil
		}
		return swarm.Service{}, false, err
	}
	return result.Service, true, nil
}

func (manager *serviceManager) imageDigest(name string) (string, error) {
	if name == "" || !manager.resolveImage {
		return name, nil
	}
	reference := docker.ParseImageReference(name)
	image := name
	if reference.Digest == "" && reference.Tag == "" {
		image = name + ":latest"
	}
	result, err := manager.cli.DistributionInspect(manager.ctx, image, client.DistributionInspectOptions{})
	if err != nil {
		return "", err
	}
	digest := result.Descriptor.Digest.String()
	if digest == "" {
		return image, nil
	}
	return image + "@" + digest, nil
}

func (manager *serviceManager) secretIDs() (map[string]string, error) {
	names := make([]string, 0, len(manager.req.Secrets))
	for _, secret := range manager.req.Secrets {
		if secret.SecretID == "" && secret.SecretName != "" {
			names = append(names, secret.SecretName)
		}
	}
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	filters := make(client.Filters)
	filters.Add("name", names...)
	result, err := manager.cli.SecretList(manager.ctx, client.SecretListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, secret := range result.Items {
		if wanted[secret.Spec.Name] {
			ids[secret.Spec.Name] = secret.ID
		}
	}
	for _, name := range names {
		if _, found := ids[name]; !found {
			return nil, fmt.Errorf("Could not find a secret named %q", name)
		}
	}
	return ids, nil
}

func (manager *serviceManager) configIDs() (map[string]string, error) {
	names := make([]string, 0, len(manager.req.Configs))
	for _, config := range manager.req.Configs {
		if config.ConfigID == "" && config.ConfigName != "" {
			names = append(names, config.ConfigName)
		}
	}
	if len(names) == 0 {
		return map[string]string{}, nil
	}
	filters := make(client.Filters)
	filters.Add("name", names...)
	result, err := manager.cli.ConfigList(manager.ctx, client.ConfigListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, config := range result.Items {
		if wanted[config.Spec.Name] {
			ids[config.Spec.Name] = config.ID
		}
	}
	for _, name := range names {
		if _, found := ids[name]; !found {
			return nil, fmt.Errorf("Could not find a config named %q", name)
		}
	}
	return ids, nil
}

func (manager *serviceManager) networkIDs() (map[string]string, error) {
	result, err := manager.cli.NetworkList(manager.ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	scopes := map[string]string{}
	for _, item := range result.Items {
		if current, found := ids[item.Name]; found {
			if item.Scope == "swarm" && scopes[item.Name] != "swarm" {
				ids[item.Name] = item.ID
				scopes[item.Name] = item.Scope
			}
			_ = current
			continue
		}
		ids[item.Name] = item.ID
		scopes[item.Name] = item.Scope
	}
	return ids, nil
}

func (manager *serviceManager) networkName(id string) string {
	if id == "" || id == "<nil>" {
		return id
	}
	if manager.networkNames == nil {
		manager.networkNames = map[string]string{}
	}
	if name, found := manager.networkNames[id]; found {
		return name
	}
	result, err := manager.cli.NetworkInspect(manager.ctx, id, client.NetworkInspectOptions{})
	if err != nil || strings.TrimSpace(result.Network.Name) == "" {
		manager.networkNames[id] = id
		return id
	}
	manager.networkNames[id] = result.Network.Name
	return result.Network.Name
}

func (manager *serviceManager) withCanonicalNetworks(state *serviceState) *serviceState {
	if state == nil {
		return nil
	}
	clone := *state
	if state.networks == nil {
		return &clone
	}
	clone.networks = copyNetworkList(state.networks)
	for _, net := range clone.networks {
		id := fmt.Sprint(net["id"])
		if id == "" || id == "<nil>" {
			continue
		}
		net["id"] = manager.networkName(id)
	}
	return &clone
}

func (manager *serviceManager) create(desired *serviceState) Response {
	if !manager.checkMode {
		spec := desired.buildCreateSpec(manager.req.Name)
		result, err := manager.cli.ServiceCreate(manager.ctx, client.ServiceCreateOptions{Spec: spec})
		if err != nil {
			return failedResponse(err.Error())
		}
		desired.serviceID = result.ID
	}
	return manager.success("Service created", true, false, nil, desired, map[string]any{}, desired.facts())
}

func (manager *serviceManager) update(existing swarm.Service, current, desired *serviceState) Response {
	comparison, err := manager.withCanonicalNetworks(desired).compare(manager.withCanonicalNetworks(current))
	if err != nil {
		return failedResponse(err.Error())
	}
	if !comparison.changed && !comparison.forceUpdate {
		return manager.success("Service unchanged", false, false, nil, desired, comparison.before, comparison.after)
	}

	msg := "Service updated"
	rebuilt := false
	if comparison.needsRebuild {
		msg = "Service rebuilt"
		rebuilt = true
	} else if len(comparison.changes) == 0 && comparison.forceUpdate {
		msg = "Service forcefully updated"
	}

	if !manager.checkMode {
		if comparison.needsRebuild {
			if _, err := manager.cli.ServiceRemove(manager.ctx, existing.ID, client.ServiceRemoveOptions{}); err != nil {
				return failedResponse(err.Error())
			}
			spec := desired.buildCreateSpec(manager.req.Name)
			result, err := manager.cli.ServiceCreate(manager.ctx, client.ServiceCreateOptions{Spec: spec})
			if err != nil {
				return failedResponse(err.Error())
			}
			desired.serviceID = result.ID
		} else {
			spec := desired.applyToSpec(existing.Spec, manager.req.Name)
			_, err := manager.cli.ServiceUpdate(manager.ctx, existing.ID, client.ServiceUpdateOptions{
				Version: existing.Version,
				Spec:    spec,
			})
			if err != nil {
				return failedResponse(err.Error())
			}
			desired.serviceID = existing.ID
		}
	} else {
		desired.serviceID = existing.ID
	}
	return manager.success(msg, true, rebuilt, comparison.changes, desired, comparison.before, comparison.after)
}

func (manager *serviceManager) remove(current *serviceState) Response {
	if !manager.checkMode {
		if _, err := manager.cli.ServiceRemove(manager.ctx, manager.req.Name, client.ServiceRemoveOptions{}); err != nil {
			return failedResponse(err.Error())
		}
	}
	return manager.success("Service removed", true, false, nil, current, current.facts(), map[string]any{})
}

func (manager *serviceManager) success(msg string, changed, rebuilt bool, changes []string, desired *serviceState, before, after map[string]any) Response {
	if changes == nil {
		changes = []string{}
	}
	response := Response{
		Changed:      changed,
		Msg:          msg,
		Rebuilt:      rebuilt,
		Changes:      changes,
		SwarmService: desired.facts(),
		ServiceID:    desired.serviceID,
	}
	if !changed {
		if manager.req.State == "absent" {
			response.SwarmService = map[string]any{}
		}
	}
	if manager.req.State == "absent" && msg == "Service absent" {
		response.SwarmService = nil
	}
	if manager.diffMode {
		response.Diff = &Diff{Before: before, After: after}
	}
	return response
}
