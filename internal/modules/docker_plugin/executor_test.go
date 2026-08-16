package docker_plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/plugin"
	"github.com/moby/moby/api/types/registry"
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
		Environment: docker.StaticEnvironment{"DOCKER_CONFIG": "/nonexistent/dibra-plugin-unit-test"},
		FileSystem:  docker.OSFileSystem{},
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

func TestPluginInstallReadsRegistryAuthenticationFromDockerConfig(t *testing.T) {
	directory := t.TempDir()
	credentials := base64.StdEncoding.EncodeToString([]byte("config-user:config-pass"))
	if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(
		`{"auths":{"registry.example.test:5000":{"auth":"`+credentials+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	dependencies := pluginDependencies(fake)
	dependencies.Environment = docker.StaticEnvironment{"DOCKER_CONFIG": directory}
	dependencies.FileSystem = docker.OSFileSystem{}
	response := ExecuteWithDependencies(Request{PluginName: "registry.example.test:5000/team/plugin:latest"}, dependencies)
	if response.Failed || len(fake.installed) != 1 {
		t.Fatalf("response = %#v installed=%#v", response, fake.installed)
	}
	raw, err := base64.URLEncoding.DecodeString(fake.installed[0].RegistryAuth)
	if err != nil {
		t.Fatal(err)
	}
	var auth registry.AuthConfig
	if err := json.Unmarshal(raw, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.Username != "config-user" || auth.Password != "config-pass" {
		t.Fatalf("auth = %#v", auth)
	}
}

func TestPluginPresentCheckModeDoesNotInstall(t *testing.T) {
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{}}
	response := ExecuteWithDependenciesAndState(Request{PluginName: "vieux/sshfs"}, pluginDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || len(fake.installed) != 0 {
		t.Fatalf("response = %#v", response)
	}
	if response.Actions == nil || len(*response.Actions) != 1 || response.Diff == nil || response.Diff.After["exists"] != true {
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

func TestPluginDebugReturnsPresentStateActions(t *testing.T) {
	debug := true
	fake := &pluginClient{plugins: map[string]client.PluginInspectResult{
		"sshfs": inspectPlugin("sshfs", true, []string{"DEBUG=0"}),
	}}
	response := ExecuteWithDependencies(Request{
		PluginName:    "vieux/sshfs",
		Alias:         "sshfs",
		PluginOptions: PluginOptions{"DEBUG": "0"},
		CommonArgs:    docker.CommonArgs{Debug: &debug},
	}, pluginDependencies(fake))
	if response.Failed || response.Changed || response.Actions == nil || len(*response.Actions) != 0 {
		t.Fatalf("response = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"actions":[]`) {
		t.Fatalf("encoded response = %s", encoded)
	}
}

func TestPreparePluginOptionsMatchesUpstreamValueConversion(t *testing.T) {
	options := PluginOptions{
		"BOOL_FALSE": false,
		"BOOL_TRUE":  true,
		"EMPTY":      nil,
		"FLOAT":      json.Number("1.5"),
		"INTEGER":    json.Number("1"),
		"LIST":       []any{"value", true, nil},
		"STRING":     "text",
	}
	got, err := prepareOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"BOOL_FALSE=False",
		"BOOL_TRUE=True",
		"EMPTY=",
		"FLOAT=1.5",
		"INTEGER=1",
		"LIST=['value', True, None]",
		"STRING=text",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("prepareOptions() = %#v, want %#v", got, want)
	}
}

func TestPluginOptionDifferencesMatchUpstreamFalseyAndTypedComparison(t *testing.T) {
	existing := map[string]any{"Settings": map[string]any{
		"Env": []any{"STRING=0", "EMPTY=", "INTEGER=1"},
	}}
	requested := PluginOptions{
		"STRING":  "0",
		"EMPTY":   "",
		"INTEGER": json.Number("1"),
	}
	differences := optionDifferences(requested, existing)
	if _, found := differences["plugin_options.STRING"]; found {
		t.Fatalf("matching string should be idempotent: %#v", differences)
	}
	if differences["plugin_options.EMPTY"][1] != "" {
		t.Fatalf("falsey option difference = %#v", differences)
	}
	if differences["plugin_options.INTEGER"][1] != json.Number("1") {
		t.Fatalf("typed option difference = %#v", differences)
	}

	missingSettings := optionDifferences(PluginOptions{"DEBUG": "1"}, map[string]any{})
	if _, found := missingSettings["plugin_options"]; !found {
		t.Fatalf("missing settings difference = %#v", missingSettings)
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
