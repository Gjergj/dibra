package docker_network_info

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type fakeNetworkInfoClient struct {
	client.APIClient
	inspectedName string
	verbose       bool
	closed        bool
	result        client.NetworkInspectResult
	err           error
}

func (fake *fakeNetworkInfoClient) NetworkInspect(_ context.Context, name string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	fake.inspectedName = name
	fake.verbose = options.Verbose
	return fake.result, fake.err
}

func (fake *fakeNetworkInfoClient) Close() error {
	fake.closed = true
	return nil
}

func TestExecuteWithDependenciesUsesRawInspectKeys(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "network-id"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.EnableIPv6 = true
	inspect.Internal = true
	inspect.Scope = "local"
	raw, err := json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNetworkInfoClient{result: client.NetworkInspectResult{Network: inspect, Raw: raw}}
	response := ExecuteWithDependencies(Request{Name: "web"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{"DOCKER_TIMEOUT": "5"},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	})

	if response.Failed || !response.Exists || response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if fake.inspectedName != "web" || !fake.verbose || !fake.closed {
		t.Errorf("injected client calls = inspected %q verbose %t closed %t", fake.inspectedName, fake.verbose, fake.closed)
	}
	if response.Network["Id"] != "network-id" || response.Network["Name"] != "web" || response.Network["Driver"] != "bridge" {
		t.Errorf("raw inspection identity = %#v", response.Network)
	}
	for _, key := range []string{"Id", "Name", "Driver", "IPAM", "Containers", "EnableIPv6"} {
		if _, found := response.Network[key]; !found {
			t.Errorf("raw inspection key %q missing from %#v", key, response.Network)
		}
	}
	for _, divergentKey := range []string{"id", "name", "driver", "enable_ipv6"} {
		if _, found := response.Network[divergentKey]; found {
			t.Errorf("legacy transformed key %q remains in %#v", divergentKey, response.Network)
		}
	}

	decoded, err := docker.DecodeInspection(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Network, decoded) {
		t.Errorf("network inspection differs from Engine JSON:\ngot  %#v\nwant %#v", response.Network, decoded)
	}
}

func TestExecuteMissingNetworkReturnsExistsFalseAndNullNetwork(t *testing.T) {
	fake := &fakeNetworkInfoClient{err: errdefs.ErrNotFound.WithMessage("missing")}
	response := ExecuteWithDependencies(Request{Name: "missing"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	})
	if response.Failed || response.Changed || response.Exists || response.Network != nil || response.Msg != "" {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"exists":false`) || !strings.Contains(string(encoded), `"network":null`) {
		t.Fatalf("missing-network JSON = %s", encoded)
	}
}

func TestExecuteUnexpectedEngineErrorMatchesPinnedContract(t *testing.T) {
	fake := &fakeNetworkInfoClient{err: errors.New("daemon unavailable")}
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
			return &fakeNetworkInfoClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "name is required" || clientCreated {
		t.Fatalf("ExecuteWithDependencies() = %#v, clientCreated=%t", response, clientCreated)
	}
}

func TestExecuteFallsBackToInspectionMapWhenRawIsEmpty(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "from-struct"
	inspect.Name = "fallback"
	fake := &fakeNetworkInfoClient{result: client.NetworkInspectResult{Network: inspect}}
	response := ExecuteWithDependencies(Request{Name: "fallback"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	})
	if response.Failed || response.Network["Id"] != "from-struct" {
		t.Fatalf("fallback inspection = %#v", response)
	}
}
