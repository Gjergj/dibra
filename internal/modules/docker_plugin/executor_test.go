package docker_plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/plugin"
	"github.com/moby/moby/client"
)

type pluginClient struct {
	client.APIClient
	plugins   map[string]client.PluginInspectResult
	installed []client.PluginInstallOptions
	enabled   []string
	disabled  []string
	removed   []client.PluginRemoveOptions
	setArgs   [][]string
}

func (fake *pluginClient) PluginInspect(_ context.Context, name string, _ client.PluginInspectOptions) (client.PluginInspectResult, error) {
	result, found := fake.plugins[name]
	if !found {
		return client.PluginInspectResult{}, errdefs.ErrNotFound
	}
	return result, nil
}

func (fake *pluginClient) PluginInstall(_ context.Context, name string, options client.PluginInstallOptions) (client.PluginInstallResult, error) {
	fake.installed = append(fake.installed, options)
	raw, _ := json.Marshal(plugin.Plugin{Name: name, Enabled: false, Settings: plugin.Settings{Env: options.Args}})
	fake.plugins[name] = client.PluginInspectResult{
		Plugin: plugin.Plugin{Name: name, Enabled: false, Settings: plugin.Settings{Env: options.Args}},
		Raw:    raw,
	}
	return client.PluginInstallResult{ReadCloser: io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`)))}, nil
}

func (fake *pluginClient) PluginEnable(_ context.Context, name string, _ client.PluginEnableOptions) (client.PluginEnableResult, error) {
	fake.enabled = append(fake.enabled, name)
	current := fake.plugins[name]
	current.Plugin.Enabled = true
	raw, _ := json.Marshal(current.Plugin)
	current.Raw = raw
	fake.plugins[name] = current
	return client.PluginEnableResult{}, nil
}

func (fake *pluginClient) PluginDisable(_ context.Context, name string, _ client.PluginDisableOptions) (client.PluginDisableResult, error) {
	fake.disabled = append(fake.disabled, name)
	current := fake.plugins[name]
	current.Plugin.Enabled = false
	raw, _ := json.Marshal(current.Plugin)
	current.Raw = raw
	fake.plugins[name] = current
	return client.PluginDisableResult{}, nil
}

func (fake *pluginClient) PluginRemove(_ context.Context, name string, options client.PluginRemoveOptions) (client.PluginRemoveResult, error) {
	fake.removed = append(fake.removed, options)
	delete(fake.plugins, name)
	return client.PluginRemoveResult{}, nil
}

func (fake *pluginClient) PluginSet(_ context.Context, name string, options client.PluginSetOptions) (client.PluginSetResult, error) {
	fake.setArgs = append(fake.setArgs, options.Args)
	current := fake.plugins[name]
	current.Plugin.Settings.Env = options.Args
	raw, _ := json.Marshal(current.Plugin)
	current.Raw = raw
	fake.plugins[name] = current
	return client.PluginSetResult{}, nil
}

func (*pluginClient) Close() error { return nil }

func pluginDependencies(fake *pluginClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func inspectPlugin(name string, enabled bool, env []string) client.PluginInspectResult {
	item := plugin.Plugin{Name: name, Enabled: enabled, Settings: plugin.Settings{Env: env}}
	raw, _ := json.Marshal(item)
	return client.PluginInspectResult{Plugin: item, Raw: raw}
}

func TestPluginPresentInstallsAndOmitsActions(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependencies(Request{PluginName: "vieux/sshfs"}, pluginDependencies(fake))
	if response.Failed || !response.Changed || len(fake.installed) != 1 || response.Actions != nil {
		t.Fatalf("response = %#v installed=%#v", response, fake.installed)
	}
	if !fake.installed[0].Disabled || !fake.installed[0].AcceptAllPermissions {
		t.Fatalf("install options = %#v", fake.installed[0])
	}
	if response.Plugin["Name"] != "vieux/sshfs" {
		t.Fatalf("plugin = %#v", response.Plugin)
	}
}

func TestPluginPresentCheckModeDoesNotInstall(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependenciesAndState(Request{PluginName: "vieux/sshfs"}, pluginDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || len(fake.installed) != 0 {
		t.Fatalf("response = %#v", response)
	}
	if len(response.Actions) != 1 || response.Diff == nil || response.Diff.After["exists"] != true {
		t.Fatalf("check/diff = %#v", response)
	}
}

func TestPluginUpdatesOptions(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{
		"sshfs": inspectPlugin("sshfs", true, []string{"DEBUG=0"}),
	}}
	response := ExecuteWithDependencies(Request{
		PluginName:    "vieux/sshfs",
		Alias:         "sshfs",
		PluginOptions: map[string]any{"DEBUG": "1"},
	}, pluginDependencies(fake))
	if response.Failed || !response.Changed || len(fake.setArgs) != 1 {
		t.Fatalf("response = %#v set=%#v", response, fake.setArgs)
	}
}

func TestPluginDisableMissingFails(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependencies(Request{PluginName: "missing", State: "disable"}, pluginDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "Plugin not found: Plugin does not exist.") {
		t.Fatalf("response = %#v", response)
	}
}

func TestPluginEnableInstallsWhenMissing(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependencies(Request{PluginName: "vieux/sshfs", State: "enable"}, pluginDependencies(fake))
	if response.Failed || !response.Changed || len(fake.installed) != 1 || len(fake.enabled) != 1 {
		t.Fatalf("response = %#v installed=%#v enabled=%#v", response, fake.installed, fake.enabled)
	}
}

func TestPluginAbsentIsIdempotentAndForceRemove(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependencies(Request{PluginName: "missing", State: "absent"}, pluginDependencies(fake))
	if response.Failed || response.Changed {
		t.Fatalf("missing absent = %#v", response)
	}

	fake.plugins["sshfs"] = inspectPlugin("sshfs", true, nil)
	removed := ExecuteWithDependencies(Request{PluginName: "sshfs", State: "absent", ForceRemove: true}, pluginDependencies(fake))
	if removed.Failed || !removed.Changed || len(fake.removed) != 1 || !fake.removed[0].Force {
		t.Fatalf("remove = %#v %#v", removed, fake.removed)
	}
}
