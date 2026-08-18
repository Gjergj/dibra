package docker_volume

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type volumeClient struct {
	client.APIClient
	volumes   map[string]client.VolumeInspectResult
	created   []client.VolumeCreateOptions
	removed   []string
	force     []bool
	createErr error
	removeErr error
}

func (fake *volumeClient) VolumeInspect(_ context.Context, name string, _ client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	result, found := fake.volumes[name]
	if !found {
		return client.VolumeInspectResult{}, fmt.Errorf("%w: volume %s not found", errdefs.ErrNotFound, name)
	}
	return result, nil
}

func (fake *volumeClient) VolumeCreate(_ context.Context, options client.VolumeCreateOptions) (client.VolumeCreateResult, error) {
	fake.created = append(fake.created, options)
	if fake.createErr != nil {
		return client.VolumeCreateResult{}, fake.createErr
	}
	if options.Name == "" {
		options.Name = "generated"
	}
	inspect := volumeInspect(options.Name, options.Driver, options.Labels, options.DriverOpts)
	if fake.volumes == nil {
		fake.volumes = map[string]client.VolumeInspectResult{}
	}
	fake.volumes[options.Name] = inspect
	return client.VolumeCreateResult{Volume: inspect.Volume}, nil
}

func (fake *volumeClient) VolumeRemove(_ context.Context, name string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	fake.removed = append(fake.removed, name)
	fake.force = append(fake.force, options.Force)
	if fake.removeErr != nil {
		return client.VolumeRemoveResult{}, fake.removeErr
	}
	delete(fake.volumes, name)
	return client.VolumeRemoveResult{}, nil
}

func (*volumeClient) Close() error { return nil }

func volumeInspect(name, driver string, labels, options map[string]string) client.VolumeInspectResult {
	if driver == "" {
		driver = "local"
	}
	payload := map[string]any{
		"Name":       name,
		"Driver":     driver,
		"Mountpoint": "/var/lib/docker/volumes/" + name + "/_data",
		"CreatedAt":  "2018-12-09T17:43:44+01:00",
		"Scope":      "local",
		"Labels":     labels,
		"Options":    options,
	}
	raw, _ := json.Marshal(payload)
	copiedLabels := labels
	if labels != nil {
		copiedLabels = docker.NormalizeLabels(labels)
	}
	copiedOptions := options
	if options != nil {
		copiedOptions = docker.NormalizeLabels(options)
	}
	return client.VolumeInspectResult{
		Volume: volume.Volume{
			Name:       name,
			Driver:     driver,
			Mountpoint: payload["Mountpoint"].(string),
			CreatedAt:  "2018-12-09T17:43:44+01:00",
			Scope:      "local",
			Labels:     copiedLabels,
			Options:    copiedOptions,
		},
		Raw: raw,
	}
}

func volumeDependencies(fake *volumeClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestMissingNameFailsBeforeClient(t *testing.T) {
	created := false
	response := ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return &volumeClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "volume_name is required" || created {
		t.Fatalf("response = %#v created=%t", response, created)
	}
}

func TestNameFallbackIsUsedWhenVolumeNameEmpty(t *testing.T) {
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{}}
	response := ExecuteWithDependencies(Request{Name: "from-name"}, volumeDependencies(fake))
	if response.Failed || !response.Changed || len(fake.created) != 1 || fake.created[0].Name != "from-name" {
		t.Fatalf("response = %#v created=%#v", response, fake.created)
	}
}

func TestCheckModeCreateDoesNotMutate(t *testing.T) {
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{}}
	request := Request{VolumeName: "data"}
	response := ExecuteWithDependenciesAndState(request, volumeDependencies(fake), execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || len(fake.created) != 0 || response.Volume != nil {
		t.Fatalf("response = %#v created=%#v", response, fake.created)
	}
	if response.Diff == nil || response.Diff.Before["exists"] != false || response.Diff.After["exists"] != true {
		t.Fatalf("diff = %#v", response.Diff)
	}
}

func TestIdempotentPresentLeavesExistingVolume(t *testing.T) {
	existing := volumeInspect("data", "local", map[string]string{"env": "test"}, map[string]string{"type": "tmpfs"})
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	request := Request{VolumeName: "data", Driver: "local", DriverOptions: map[string]string{"type": "tmpfs"}, Labels: map[string]string{"env": "test"}}
	request.SetProvidedArguments([]string{"volume_name", "driver", "driver_options", "labels"})
	response := ExecuteWithDependencies(request, volumeDependencies(fake))
	if response.Failed || response.Changed || len(fake.created) != 0 || len(fake.removed) != 0 {
		t.Fatalf("response = %#v created=%#v removed=%#v", response, fake.created, fake.removed)
	}
	if response.Volume["Name"] != "data" || response.Volume["Driver"] != "local" {
		t.Fatalf("volume = %#v", response.Volume)
	}
}

func TestNeverLeavesMismatchedVolumeUnchanged(t *testing.T) {
	existing := volumeInspect("data", "local", map[string]string{"env": "test"}, map[string]string{"type": "tmpfs", "o": "size=100m"})
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	request := Request{
		VolumeName:    "data",
		Driver:        "local",
		DriverOptions: map[string]string{"type": "tmpfs", "o": "size=200m"},
		Labels:        map[string]string{"env": "prod"},
	}
	response := ExecuteWithDependenciesAndState(request, volumeDependencies(fake), execution.State{DiffMode: true})
	if response.Failed || response.Changed || len(fake.created) != 0 || len(fake.removed) != 0 {
		t.Fatalf("response = %#v created=%#v removed=%#v", response, fake.created, fake.removed)
	}
	if response.Volume["Name"] != "data" || fake.volumes["data"].Volume.Options["o"] != "size=100m" {
		t.Fatalf("existing volume was mutated: %#v", fake.volumes["data"])
	}
	if response.Diff == nil || response.Diff.After["driver_options.o"] != "size=200m" || response.Diff.After["labels.env"] != "prod" {
		t.Fatalf("diff = %#v", response.Diff)
	}
}

func TestOptionsChangedRecreatesOnDriverOptionsAndLabels(t *testing.T) {
	existing := volumeInspect("data", "local", map[string]string{"env": "test"}, map[string]string{"type": "tmpfs", "o": "size=100m"})
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	request := Request{
		VolumeName:    "data",
		Driver:        "local",
		DriverOptions: map[string]string{"type": "tmpfs", "o": "size=200m"},
		Recreate:      "options-changed",
	}
	response := ExecuteWithDependencies(request, volumeDependencies(fake))
	if response.Failed || !response.Changed || len(fake.removed) != 1 || len(fake.created) != 1 || fake.force[0] {
		t.Fatalf("driver_options response = %#v removed=%#v created=%#v force=%#v", response, fake.removed, fake.created, fake.force)
	}
	if fake.volumes["data"].Volume.Options["o"] != "size=200m" {
		t.Fatalf("recreated options = %#v", fake.volumes["data"].Volume.Options)
	}

	same := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": fake.volumes["data"]}}
	sameRequest := Request{
		VolumeName:    "data",
		Driver:        "local",
		DriverOptions: map[string]string{"type": "tmpfs", "o": "size=200m"},
		Recreate:      "options-changed",
	}
	sameResponse := ExecuteWithDependencies(sameRequest, volumeDependencies(same))
	if sameResponse.Failed || sameResponse.Changed || len(same.removed) != 0 {
		t.Fatalf("identical options-changed = %#v removed=%#v", sameResponse, same.removed)
	}

	labeled := volumeInspect("data", "local", map[string]string{"env": "test"}, nil)
	labelFake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": labeled}}
	labelRequest := Request{
		VolumeName: "data",
		Labels:     map[string]string{"env": "test", "app": "dibra"},
		Recreate:   "options-changed",
	}
	labelResponse := ExecuteWithDependencies(labelRequest, volumeDependencies(labelFake))
	if labelResponse.Failed || !labelResponse.Changed || len(labelFake.removed) != 1 || len(labelFake.created) != 1 {
		t.Fatalf("labels response = %#v removed=%#v created=%#v", labelResponse, labelFake.removed, labelFake.created)
	}

	subset := &volumeClient{volumes: map[string]client.VolumeInspectResult{
		"data": volumeInspect("data", "local", map[string]string{"env": "test", "app": "dibra"}, nil),
	}}
	subsetRequest := Request{VolumeName: "data", Labels: map[string]string{"env": "test"}, Recreate: "options-changed"}
	subsetResponse := ExecuteWithDependencies(subsetRequest, volumeDependencies(subset))
	if subsetResponse.Failed || subsetResponse.Changed || len(subset.removed) != 0 {
		t.Fatalf("subset labels should not recreate = %#v removed=%#v", subsetResponse, subset.removed)
	}
}

func TestAlwaysRecreatesExistingVolume(t *testing.T) {
	existing := volumeInspect("data", "local", nil, nil)
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	request := Request{VolumeName: "data", Recreate: "always"}
	response := ExecuteWithDependenciesAndState(request, volumeDependencies(fake), execution.State{DiffMode: true})
	if response.Failed || !response.Changed || len(fake.removed) != 1 || len(fake.created) != 1 {
		t.Fatalf("response = %#v removed=%#v created=%#v", response, fake.removed, fake.created)
	}
	if response.Volume["Name"] != "data" || response.Diff.Before["exists"] != true || response.Diff.After["exists"] != true {
		t.Fatalf("result = %#v", response)
	}
}

func TestAbsentRemovesExistingAndIsIdempotentWhenMissing(t *testing.T) {
	existing := volumeInspect("data", "local", nil, nil)
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	response := ExecuteWithDependenciesAndState(Request{VolumeName: "data", State: "absent"}, volumeDependencies(fake), execution.State{DiffMode: true})
	if response.Failed || !response.Changed || len(fake.removed) != 1 || fake.force[0] || response.Volume != nil {
		t.Fatalf("remove = %#v removed=%#v force=%#v", response, fake.removed, fake.force)
	}
	if response.Diff.Before["exists"] != true || response.Diff.After["exists"] != false {
		t.Fatalf("diff = %#v", response.Diff)
	}

	missing := ExecuteWithDependencies(Request{Name: "data", State: "absent"}, volumeDependencies(fake))
	if missing.Failed || missing.Changed || len(fake.removed) != 1 {
		t.Fatalf("idempotent absent = %#v removed=%#v", missing, fake.removed)
	}
}

func TestCheckModeAbsentDoesNotRemove(t *testing.T) {
	existing := volumeInspect("data", "local", nil, nil)
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{"data": existing}}
	response := ExecuteWithDependenciesAndState(Request{VolumeName: "data", State: "absent"}, volumeDependencies(fake), execution.State{CheckMode: true})
	if response.Failed || !response.Changed || len(fake.removed) != 0 || fake.volumes["data"].Volume.Name != "data" {
		t.Fatalf("response = %#v removed=%#v", response, fake.removed)
	}
}

func TestOmittedDriverDefaultsToLocal(t *testing.T) {
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{}}
	response := ExecuteWithDependencies(Request{VolumeName: "data"}, volumeDependencies(fake))
	if response.Failed || fake.created[0].Driver != "local" {
		t.Fatalf("response = %#v created=%#v", response, fake.created)
	}

	explicit := &volumeClient{volumes: map[string]client.VolumeInspectResult{}}
	request := Request{VolumeName: "data", Driver: ""}
	request.SetProvidedArguments([]string{"volume_name", "driver"})
	response = ExecuteWithDependencies(request, volumeDependencies(explicit))
	if response.Failed || explicit.created[0].Driver != "" {
		t.Fatalf("explicit empty driver = %#v created=%#v", response, explicit.created)
	}
}

func TestRawInspectKeysOnCreate(t *testing.T) {
	fake := &volumeClient{volumes: map[string]client.VolumeInspectResult{}}
	response := ExecuteWithDependencies(Request{VolumeName: "data", Labels: map[string]string{"env": "test"}}, volumeDependencies(fake))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	for _, key := range []string{"Name", "Driver", "Mountpoint", "CreatedAt", "Scope", "Labels", "Options"} {
		if _, found := response.Volume[key]; !found {
			t.Fatalf("raw key %q missing from %#v", key, response.Volume)
		}
	}
	if _, found := response.Volume["name"]; found {
		t.Fatalf("snake_case leaked: %#v", response.Volume)
	}
}

func TestInvalidStateAndRecreate(t *testing.T) {
	response := ExecuteWithDependencies(Request{VolumeName: "data", State: "running"}, docker.Dependencies{})
	if !response.Failed || !strings.Contains(response.Msg, "state must be present or absent") {
		t.Fatalf("state = %#v", response)
	}
	response = ExecuteWithDependencies(Request{VolumeName: "data", Recreate: "sometimes"}, docker.Dependencies{})
	if !response.Failed || !strings.Contains(response.Msg, "recreate must be never, always, or options-changed") {
		t.Fatalf("recreate = %#v", response)
	}
}

func TestLabelMapMatchesUpstreamSanitization(t *testing.T) {
	var request Request
	err := json.Unmarshal([]byte(`{"volume_name":"data","labels":{"string":"value","integer":2,"empty":null}}`), &request)
	if err != nil {
		t.Fatalf("valid labels failed: %v", err)
	}
	expected := LabelMap{"string": "value", "integer": "2", "empty": ""}
	if len(request.Labels) != len(expected) {
		t.Fatalf("labels = %#v, want %#v", request.Labels, expected)
	}
	for key, value := range expected {
		if request.Labels[key] != value {
			t.Fatalf("labels = %#v, want %#v", request.Labels, expected)
		}
	}

	for _, input := range []string{
		`{"volume_name":"data","labels":{"foo":1.0}}`,
		`{"volume_name":"data","labels":{"foo":true}}`,
	} {
		err = json.Unmarshal([]byte(input), &request)
		if err == nil || !strings.Contains(err.Error(), "of labels is not a string or something than can be safely converted to a string!") {
			t.Fatalf("invalid labels error = %v", err)
		}
	}
}
