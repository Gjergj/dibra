package docker_swarm_service_info

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

type fakeServiceInfoClient struct {
	client.APIClient
	manager        bool
	closed         bool
	inspectedName  string
	swarmInspected bool
	result         client.ServiceInspectResult
	inspectErr     error
}

func (fake *fakeServiceInfoClient) Close() error {
	fake.closed = true
	return nil
}

func (fake *fakeServiceInfoClient) SwarmInspect(context.Context, client.SwarmInspectOptions) (client.SwarmInspectResult, error) {
	fake.swarmInspected = true
	if !fake.manager {
		return client.SwarmInspectResult{}, errors.New("This node is not a swarm manager")
	}
	return client.SwarmInspectResult{Swarm: swarm.Swarm{}}, nil
}

func (fake *fakeServiceInfoClient) ServiceInspect(_ context.Context, name string, _ client.ServiceInspectOptions) (client.ServiceInspectResult, error) {
	fake.inspectedName = name
	return fake.result, fake.inspectErr
}

func serviceInfoDependencies(fake *fakeServiceInfoClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_TIMEOUT": "5"},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func sampleService(id, name string) swarm.Service {
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name, Labels: map[string]string{"env": "test"}},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{},
			},
		},
	}
}

func managerClient(service swarm.Service, raw []byte) *fakeServiceInfoClient {
	if raw == nil {
		raw, _ = json.Marshal(service)
	}
	return &fakeServiceInfoClient{
		manager: true,
		result:  client.ServiceInspectResult{Service: service, Raw: raw},
	}
}

func TestExecuteRejectsMissingNameBeforeCreatingClient(t *testing.T) {
	clientCreated := false
	response := ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			clientCreated = true
			return &fakeServiceInfoClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "missing required arguments: name" || clientCreated {
		t.Fatalf("ExecuteWithDependencies() = %#v, clientCreated=%t", response, clientCreated)
	}
}

func TestExecuteRejectsWhitespaceNameBeforeCreatingClient(t *testing.T) {
	clientCreated := false
	response := ExecuteWithDependencies(Request{Name: "  "}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			clientCreated = true
			return &fakeServiceInfoClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "missing required arguments: name" || clientCreated {
		t.Fatalf("ExecuteWithDependencies() = %#v, clientCreated=%t", response, clientCreated)
	}
}

func TestExecuteFailsWhenNotSwarmManager(t *testing.T) {
	fake := &fakeServiceInfoClient{manager: false}
	response := ExecuteWithDependencies(Request{Name: "web"}, serviceInfoDependencies(fake))
	if !response.Failed || response.Msg != docker.NotSwarmManagerMsg || fake.inspectedName != "" {
		t.Fatalf("response = %#v inspected=%q", response, fake.inspectedName)
	}
	if !fake.swarmInspected || !fake.closed {
		t.Fatalf("swarm inspect/close = inspected %t closed %t", fake.swarmInspected, fake.closed)
	}
}

func TestExecuteUsesRawInspectKeys(t *testing.T) {
	service := sampleService("svc-id", "web")
	raw := []byte(`{"ID":"svc-id","Spec":{"Name":"web","Labels":{"env":"test"},"TaskTemplate":{"ContainerSpec":{"Image":"alpine:latest"}}}}`)
	fake := managerClient(service, raw)
	response := ExecuteWithDependencies(Request{Name: "web"}, serviceInfoDependencies(fake))
	if response.Failed || !response.Exists || response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if fake.inspectedName != "web" || !fake.closed {
		t.Errorf("injected client calls = inspected %q closed %t", fake.inspectedName, fake.closed)
	}
	decoded, err := docker.DecodeInspection(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Service, decoded) {
		t.Errorf("service inspection differs from Engine JSON:\ngot  %#v\nwant %#v", response.Service, decoded)
	}
	if response.Service["ID"] != "svc-id" {
		t.Errorf("ID = %#v", response.Service["ID"])
	}
	spec, _ := response.Service["Spec"].(map[string]any)
	if spec["Name"] != "web" {
		t.Errorf("Spec.Name = %#v", spec["Name"])
	}
}

func TestExecuteFallsBackToInspectionMapWhenRawIsEmpty(t *testing.T) {
	service := sampleService("from-struct", "fallback")
	fake := managerClient(service, []byte{})
	fake.result.Raw = nil
	response := ExecuteWithDependencies(Request{Name: "fallback"}, serviceInfoDependencies(fake))
	if response.Failed || !response.Exists || response.Service["ID"] != "from-struct" {
		t.Fatalf("fallback inspection = %#v", response)
	}
	spec, _ := response.Service["Spec"].(map[string]any)
	if spec["Name"] != "fallback" {
		t.Fatalf("Spec.Name = %#v", spec["Name"])
	}
}

func TestExecuteMissingServiceReturnsExistsFalseAndNullService(t *testing.T) {
	fake := &fakeServiceInfoClient{
		manager:    true,
		inspectErr: errdefs.ErrNotFound.WithMessage("No such service"),
	}
	response := ExecuteWithDependencies(Request{Name: "missing"}, serviceInfoDependencies(fake))
	if response.Failed || response.Changed || response.Exists || response.Service != nil || response.Msg != "" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exists":false`) || !strings.Contains(string(encoded), `"service":null`) {
		t.Fatalf("missing-service JSON = %s", encoded)
	}
	if strings.Contains(string(encoded), `"tasks"`) || strings.Contains(string(encoded), `"service_id"`) {
		t.Fatalf("unexpected extra fields in JSON = %s", encoded)
	}
}

func TestExecuteInspectsByServiceID(t *testing.T) {
	service := sampleService("abc123def456", "web")
	fake := managerClient(service, nil)
	response := ExecuteWithDependencies(Request{Name: "abc123def456"}, serviceInfoDependencies(fake))
	if response.Failed || !response.Exists || fake.inspectedName != "abc123def456" {
		t.Fatalf("response = %#v inspected=%q", response, fake.inspectedName)
	}
}

func TestExecuteUnavailableInspectMatchesPinnedContract(t *testing.T) {
	fake := &fakeServiceInfoClient{
		manager:    true,
		inspectErr: errdefs.ErrUnavailable.WithMessage("This node is not a swarm manager"),
	}
	response := ExecuteWithDependencies(Request{Name: "web"}, serviceInfoDependencies(fake))
	if !response.Failed || response.Msg != cannotInspectServiceMsg {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
}

func TestExecuteUnexpectedEngineErrorMatchesPinnedContract(t *testing.T) {
	fake := &fakeServiceInfoClient{
		manager:    true,
		inspectErr: errors.New("daemon unavailable"),
	}
	response := ExecuteWithDependencies(Request{Name: "web"}, serviceInfoDependencies(fake))
	if !response.Failed || response.Msg != "Error inspecting swarm service: daemon unavailable" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
}

func TestExecuteClientCreateErrorMatchesPinnedContract(t *testing.T) {
	response := ExecuteWithDependencies(Request{Name: "web"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return nil, errors.New("cannot dial unix socket")
		},
	})
	if !response.Failed || !strings.Contains(response.Msg, "An unexpected Docker error occurred: cannot dial unix socket") {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
}

func TestExecuteSuccessfulJSONOmitsExtraFields(t *testing.T) {
	fake := managerClient(sampleService("svc-id", "web"), nil)
	response := ExecuteWithDependencies(Request{Name: "web"}, serviceInfoDependencies(fake))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exists":true`) || !strings.Contains(string(encoded), `"service":{`) {
		t.Fatalf("success JSON = %s", encoded)
	}
	if strings.Contains(string(encoded), `"tasks"`) || strings.Contains(string(encoded), `"service_id"`) {
		t.Fatalf("unexpected extra fields in JSON = %s", encoded)
	}
}
