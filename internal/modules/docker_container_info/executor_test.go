package docker_container_info

import (
	"context"
	"testing"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fakeContainerInfoClient struct {
	client.APIClient
	inspectedName string
	closed        bool
}

func (fake *fakeContainerInfoClient) ContainerInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	fake.inspectedName = name
	return client.ContainerInspectResult{Container: containertypes.InspectResponse{
		ID:     "container-id",
		Name:   "/web",
		State:  &containertypes.State{Status: containertypes.StateRunning, Running: true},
		Config: &containertypes.Config{Image: "nginx:alpine"},
	}}, nil
}

func (fake *fakeContainerInfoClient) Close() error {
	fake.closed = true
	return nil
}

func TestExecuteWithDependenciesUsesInjectedAPIClient(t *testing.T) {
	fake := &fakeContainerInfoClient{}
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
	if response.Container["id"] != "container-id" {
		t.Errorf("container id = %#v", response.Container["id"])
	}
}
