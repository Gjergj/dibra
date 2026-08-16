package docker_plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	if req.PluginName == "" {
		return failedResponse("plugin_name is required")
	}
	stateName := req.State
	if stateName == "" {
		stateName = "present"
	}
	switch stateName {
	case "present", "absent", "enable", "disable":
	default:
		return failedResponse(fmt.Sprintf("state must be present, absent, enable, or disable, got %q", stateName))
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failedResponse(fmt.Sprintf("failed to create docker client: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()
	connection, _ := docker.ResolveConnectionWithEnvironment(req.CommonArgs, dependencies.Environment)

	manager := &pluginManager{
		req:          req,
		stateName:    stateName,
		checkMode:    state.CheckMode,
		diffMode:     state.DiffMode,
		debug:        connection.Debug,
		cli:          cli,
		ctx:          ctx,
		dependencies: dependencies,
		before:       map[string]any{},
		after:        map[string]any{},
		pluginName:   req.preferredName(),
	}
	return manager.run()
}

type pluginManager struct {
	req          Request
	stateName    string
	checkMode    bool
	diffMode     bool
	debug        bool
	cli          client.APIClient
	ctx          context.Context
	dependencies docker.Dependencies
	existing     map[string]any
	plugin       map[string]any
	before       map[string]any
	after        map[string]any
	actions      []string
	changed      bool
	pluginName   string
}

func (manager *pluginManager) run() Response {
	existing, err := manager.inspect()
	if err != nil {
		return failedResponse(err.Error())
	}
	manager.existing = existing

	switch manager.stateName {
	case "present":
		if err := manager.present(); err != nil {
			return failedResponse(err.Error())
		}
	case "absent":
		if err := manager.absent(); err != nil {
			return failedResponse(err.Error())
		}
	case "enable":
		if err := manager.enable(); err != nil {
			return failedResponse(err.Error())
		}
	case "disable":
		if err := manager.disable(); err != nil {
			return failedResponse(err.Error())
		}
	}

	response := Response{Changed: manager.changed, Plugin: manager.plugin}
	if manager.stateName != "present" || manager.checkMode || manager.debug {
		actions := manager.actions
		if actions == nil {
			actions = []string{}
		}
		response.Actions = &actions
	}
	if manager.diffMode {
		response.Diff = &Diff{Before: manager.before, After: manager.after}
	}
	return response
}

func (manager *pluginManager) inspect() (map[string]any, error) {
	result, err := manager.cli.PluginInspect(manager.ctx, manager.pluginName, client.PluginInspectOptions{})
	if err != nil {
		if docker.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(result.Raw) > 0 {
		return docker.DecodeInspection(result.Raw)
	}
	return docker.InspectionMap(result.Plugin)
}

func (manager *pluginManager) present() error {
	manager.before["exists"] = manager.existing != nil
	manager.after["exists"] = true
	if manager.existing != nil {
		return manager.update()
	}
	if err := manager.install(); err != nil {
		return err
	}
	return manager.refresh()
}

func (manager *pluginManager) update() error {
	differences := optionDifferences(manager.req.PluginOptions, manager.existing)
	for key, values := range differences {
		manager.before[key] = values[0]
		manager.after[key] = values[1]
	}
	if len(differences) == 0 {
		manager.plugin = manager.existing
		return nil
	}
	if !manager.checkMode {
		args, err := prepareOptions(manager.req.PluginOptions)
		if err != nil {
			return err
		}
		if _, err := manager.cli.PluginSet(manager.ctx, manager.pluginName, client.PluginSetOptions{Args: args}); err != nil {
			return err
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Updated plugin %s settings", manager.pluginName))
	manager.changed = true
	return manager.refresh()
}

func (manager *pluginManager) install() error {
	if !manager.checkMode {
		args, err := prepareOptions(manager.req.PluginOptions)
		if err != nil {
			return err
		}
		registryAuth, err := docker.ResolveRegistryAuthForImageContext(manager.ctx, manager.req.PluginName, manager.dependencies, false)
		if err != nil {
			return docker.WrapError("resolve registry authentication", manager.req.PluginName, err)
		}
		stream, err := manager.cli.PluginInstall(manager.ctx, manager.pluginName, client.PluginInstallOptions{
			Disabled:             true,
			AcceptAllPermissions: true,
			RegistryAuth:         registryAuth,
			RemoteRef:            manager.req.PluginName,
			Args:                 args,
		})
		if err != nil {
			return err
		}
		defer stream.Close()
		if err := docker.DecodeJSONStream(stream, func(json.RawMessage) error { return nil }); err != nil {
			return err
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Installed plugin %s", manager.pluginName))
	manager.changed = true
	return nil
}

func (manager *pluginManager) absent() error {
	if manager.existing == nil {
		manager.plugin = map[string]any{}
		return nil
	}
	if !manager.checkMode {
		if _, err := manager.cli.PluginRemove(manager.ctx, manager.pluginName, client.PluginRemoveOptions{Force: manager.req.ForceRemove}); err != nil {
			return err
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Removed plugin %s", manager.pluginName))
	manager.changed = true
	manager.plugin = map[string]any{}
	return nil
}

func (manager *pluginManager) enable() error {
	if manager.existing == nil {
		if err := manager.install(); err != nil {
			return err
		}
		if !manager.checkMode {
			if _, err := manager.cli.PluginEnable(manager.ctx, manager.pluginName, client.PluginEnableOptions{Timeout: manager.req.EnableTimeout}); err != nil {
				return err
			}
		}
		manager.actions = append(manager.actions, fmt.Sprintf("Enabled plugin %s", manager.pluginName))
		manager.changed = true
		return manager.refresh()
	}
	enabled, _ := manager.existing["Enabled"].(bool)
	if enabled {
		manager.plugin = manager.existing
		return nil
	}
	if !manager.checkMode {
		if _, err := manager.cli.PluginEnable(manager.ctx, manager.pluginName, client.PluginEnableOptions{Timeout: manager.req.EnableTimeout}); err != nil {
			return err
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Enabled plugin %s", manager.pluginName))
	manager.changed = true
	return manager.refresh()
}

func (manager *pluginManager) disable() error {
	if manager.existing == nil {
		return fmt.Errorf("Plugin not found: Plugin does not exist.")
	}
	enabled, _ := manager.existing["Enabled"].(bool)
	if !enabled {
		manager.plugin = manager.existing
		return nil
	}
	if !manager.checkMode {
		if _, err := manager.cli.PluginDisable(manager.ctx, manager.pluginName, client.PluginDisableOptions{}); err != nil {
			return err
		}
	}
	manager.actions = append(manager.actions, fmt.Sprintf("Disable plugin %s", manager.pluginName))
	manager.changed = true
	return manager.refresh()
}

func (manager *pluginManager) refresh() error {
	if manager.checkMode {
		if manager.existing != nil {
			manager.plugin = manager.existing
		} else {
			manager.plugin = map[string]any{}
		}
		return nil
	}
	inspected, err := manager.inspect()
	if err != nil {
		return err
	}
	if inspected == nil {
		manager.plugin = map[string]any{}
		return nil
	}
	manager.plugin = inspected
	return nil
}

func prepareOptions(options PluginOptions) ([]string, error) {
	if len(options) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, key+"="+pythonPluginOptionText(options[key], false))
	}
	return args, nil
}

func optionDifferences(requested PluginOptions, existing map[string]any) map[string][2]any {
	if len(requested) == 0 {
		return nil
	}
	settings, found := existing["Settings"]
	if !found || pythonFalsey(settings) {
		return map[string][2]any{"plugin_options": {settings, requested}}
	}
	active := parseOptions(existing)
	differences := map[string][2]any{}
	for key, value := range requested {
		activeValue := active[key]
		if (activeValue == "" && !pythonFalsey(value)) ||
			pythonFalsey(value) ||
			!pluginOptionEqualsExisting(value, activeValue) {
			differences["plugin_options."+key] = [2]any{activeValue, value}
		}
	}
	return differences
}

func pluginOptionEqualsExisting(requested any, existing string) bool {
	value, ok := requested.(string)
	return ok && value == existing
}

func pythonFalsey(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case string:
		return typed == ""
	case json.Number:
		number, err := typed.Float64()
		return err == nil && number == 0
	case float64:
		return typed == 0
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	case PluginOptions:
		return len(typed) == 0
	default:
		return false
	}
}

func pythonPluginOptionText(value any, nested bool) string {
	switch typed := value.(type) {
	case nil:
		if nested {
			return "None"
		}
		return ""
	case string:
		if nested {
			return pythonStringRepr(typed)
		}
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
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
			items[index] = pythonPluginOptionText(item, true)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]any:
		return pythonPluginOptionMapText(typed)
	case PluginOptions:
		return pythonPluginOptionMapText(map[string]any(typed))
	default:
		return fmt.Sprint(typed)
	}
}

func pythonPluginOptionMapText(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, pythonStringRepr(key)+": "+pythonPluginOptionText(values[key], true))
	}
	return "{" + strings.Join(items, ", ") + "}"
}

func pythonStringRepr(value string) string {
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

func parseOptions(plugin map[string]any) map[string]string {
	result := map[string]string{}
	settings, _ := plugin["Settings"].(map[string]any)
	if settings == nil {
		return result
	}
	env, _ := settings["Env"].([]any)
	for _, item := range env {
		text, ok := item.(string)
		if !ok {
			continue
		}
		key, value, found := strings.Cut(text, "=")
		if found {
			result[key] = value
		}
	}
	return result
}

func failedResponse(message string) Response {
	return Response{Failed: true, Msg: message}
}
