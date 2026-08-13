package docker_container_info

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fakeContainerInfoClient struct {
	client.APIClient
	inspectedName string
	closed        bool
	result        client.ContainerInspectResult
	err           error
}

func (fake *fakeContainerInfoClient) ContainerInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	fake.inspectedName = name
	return fake.result, fake.err
}

func (fake *fakeContainerInfoClient) Close() error {
	fake.closed = true
	return nil
}

func TestExecuteWithDependenciesUsesInjectedAPIClient(t *testing.T) {
	inspect := containertypes.InspectResponse{
		ID:           "container-id",
		Name:         "/web",
		Created:      "2026-08-12T17:00:00Z",
		State:        &containertypes.State{Status: containertypes.StateRunning, Running: true, Pid: 42},
		Config:       &containertypes.Config{Image: "nginx:alpine", Env: []string{"MODE=test"}},
		HostConfig:   &containertypes.HostConfig{Privileged: true},
		RestartCount: 3,
	}
	fake := &fakeContainerInfoClient{result: client.ContainerInspectResult{Container: inspect}}
	response := ExecuteWithDependencies(Request{Name: "web"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_TIMEOUT": "5"},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	})

	if response.Failed || !response.Exists {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if fake.inspectedName != "web" || !fake.closed {
		t.Errorf("injected client calls = inspected %q, closed %t", fake.inspectedName, fake.closed)
	}
	if response.Container["Id"] != "container-id" || response.Container["Name"] != "/web" {
		t.Errorf("raw inspection identity = %#v", response.Container)
	}
	for _, key := range []string{"State", "Config", "HostConfig", "NetworkSettings", "Mounts"} {
		if _, found := response.Container[key]; !found {
			t.Errorf("raw inspection key %q missing from %#v", key, response.Container)
		}
	}
	for _, divergentKey := range []string{"id", "state", "config", "host_config"} {
		if _, found := response.Container[divergentKey]; found {
			t.Errorf("legacy transformed key %q remains in %#v", divergentKey, response.Container)
		}
	}

	encoded, err := json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]interface{}
	if err := json.Unmarshal(encoded, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Container, want) {
		t.Errorf("container inspection differs from Engine JSON:\ngot  %#v\nwant %#v", response.Container, want)
	}
}

func TestExecuteMissingContainerReturnsExistsFalseAndNullContainer(t *testing.T) {
	fake := &fakeContainerInfoClient{err: errdefs.ErrNotFound.WithMessage("missing")}
	response := ExecuteWithDependencies(Request{Name: "missing"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	})
	if response.Failed || response.Changed || response.Exists || response.Container != nil || response.Msg != "" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exists":false`) || !strings.Contains(string(encoded), `"container":null`) {
		t.Fatalf("missing-container JSON = %s", encoded)
	}
}

func TestExecuteUnexpectedEngineErrorMatchesPinnedContract(t *testing.T) {
	fake := &fakeContainerInfoClient{err: errors.New("daemon unavailable")}
	response := ExecuteWithDependencies(Request{Name: "web"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	})
	if !response.Failed || !strings.Contains(response.Msg, "An unexpected Docker error occurred: daemon unavailable") {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
}

func TestExecuteRejectsMissingNameBeforeCreatingClient(t *testing.T) {
	clientCreated := false
	response := ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			clientCreated = true
			return &fakeContainerInfoClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "name is required" || clientCreated {
		t.Fatalf("ExecuteWithDependencies() = %#v, clientCreated=%t", response, clientCreated)
	}
}
