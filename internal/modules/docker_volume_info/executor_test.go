package docker_volume_info

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

type infoClient struct {
	client.APIClient
	volumes map[string]client.VolumeInspectResult
	err     error
	name    string
}

func (fake *infoClient) VolumeInspect(_ context.Context, name string, _ client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	fake.name = name
	if fake.err != nil {
		return client.VolumeInspectResult{}, fake.err
	}
	result, found := fake.volumes[name]
	if !found {
		return client.VolumeInspectResult{}, fmtNotFound(name)
	}
	return result, nil
}

func (*infoClient) Close() error { return nil }

func fmtNotFound(name string) error {
	return fmt.Errorf("%w: volume %s not found", errdefs.ErrNotFound, name)
}

func infoInspect(name string) client.VolumeInspectResult {
	payload := map[string]any{
		"Name":       name,
		"Driver":     "local",
		"Mountpoint": "/var/lib/docker/volumes/" + name + "/_data",
		"CreatedAt":  "2018-12-09T17:43:44+01:00",
		"Scope":      "local",
		"Labels":     nil,
		"Options":    map[string]any{},
	}
	raw, _ := json.Marshal(payload)
	return client.VolumeInspectResult{
		Volume: volume.Volume{
			Name:       name,
			Driver:     "local",
			Mountpoint: payload["Mountpoint"].(string),
			CreatedAt:  "2018-12-09T17:43:44+01:00",
			Scope:      "local",
			Options:    map[string]string{},
		},
		Raw: raw,
	}
}

func infoDependencies(fake *infoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func TestMissingVolumeReturnsExistsFalseAndNullVolume(t *testing.T) {
	fake := &infoClient{volumes: map[string]client.VolumeInspectResult{}}
	response := ExecuteWithDependencies(Request{Name: "missing"}, infoDependencies(fake))
	if response.Failed || response.Changed || response.Exists || response.Volume != nil || response.Msg != "" {
		t.Fatalf("response = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exists":false`) || !strings.Contains(string(encoded), `"volume":null`) {
		t.Fatalf("missing-volume JSON = %s", encoded)
	}
}

func TestPresentVolumeReturnsRawEngineInspectKeys(t *testing.T) {
	inspect := infoInspect("data")
	fake := &infoClient{volumes: map[string]client.VolumeInspectResult{"data": inspect}}
	response := ExecuteWithDependencies(Request{Name: "data"}, infoDependencies(fake))
	if response.Failed || response.Changed || !response.Exists {
		t.Fatalf("response = %#v", response)
	}
	for _, key := range []string{"Name", "Driver", "Mountpoint", "CreatedAt", "Scope", "Labels", "Options"} {
		if _, found := response.Volume[key]; !found {
			t.Fatalf("raw key %q missing from %#v", key, response.Volume)
		}
	}
	for _, key := range []string{"name", "driver", "mountpoint", "created_at"} {
		if _, found := response.Volume[key]; found {
			t.Fatalf("snake_case key %q leaked in %#v", key, response.Volume)
		}
	}
	decoded, err := docker.DecodeInspection(inspect.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Volume, decoded) {
		t.Fatalf("volume = %#v, want %#v", response.Volume, decoded)
	}
}

func TestUnexpectedInspectErrorAndMissingName(t *testing.T) {
	fake := &infoClient{err: errors.New("daemon unavailable")}
	response := ExecuteWithDependencies(Request{Name: "data"}, infoDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "An unexpected Docker error occurred: daemon unavailable") {
		t.Fatalf("inspect error = %#v", response)
	}

	created := false
	response = ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return &infoClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "name is required" || created {
		t.Fatalf("missing name = %#v created=%t", response, created)
	}
}
