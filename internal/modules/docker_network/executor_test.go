package docker_network

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type fakeNetworkClient struct {
	client.APIClient
	networks     map[string]client.NetworkInspectResult
	containers   map[string]bool
	created      []string
	createOpts   []client.NetworkCreateOptions
	removed      []string
	connected    []string
	disconnected []string
	verbose      []bool
	closed       bool
	createErr    error
	inspectErr   error
}

func (fake *fakeNetworkClient) NetworkInspect(_ context.Context, name string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	fake.verbose = append(fake.verbose, options.Verbose)
	if fake.inspectErr != nil {
		return client.NetworkInspectResult{}, fake.inspectErr
	}
	result, found := fake.networks[name]
	if !found {
		return client.NetworkInspectResult{}, errdefs.ErrNotFound.WithMessage("network not found")
	}
	return result, nil
}

func (fake *fakeNetworkClient) NetworkCreate(_ context.Context, name string, options client.NetworkCreateOptions) (client.NetworkCreateResult, error) {
	fake.created = append(fake.created, name)
	fake.createOpts = append(fake.createOpts, options)
	if fake.createErr != nil {
		return client.NetworkCreateResult{}, fake.createErr
	}
	id := "id-" + name
	inspect := network.Inspect{}
	inspect.ID = id
	inspect.Name = name
	inspect.Driver = options.Driver
	if inspect.Driver == "" {
		inspect.Driver = "bridge"
	}
	inspect.Scope = options.Scope
	if inspect.Scope == "" {
		inspect.Scope = "local"
	}
	inspect.Internal = options.Internal
	inspect.Attachable = options.Attachable
	inspect.Ingress = options.Ingress
	inspect.ConfigOnly = options.ConfigOnly
	inspect.ConfigFrom.Network = options.ConfigFrom
	inspect.Options = options.Options
	inspect.Labels = options.Labels
	inspect.Containers = map[string]network.EndpointResource{}
	if options.EnableIPv4 != nil {
		inspect.EnableIPv4 = *options.EnableIPv4
	} else {
		inspect.EnableIPv4 = true
	}
	if options.EnableIPv6 != nil {
		inspect.EnableIPv6 = *options.EnableIPv6
	}
	if options.IPAM != nil {
		inspect.IPAM = *options.IPAM
	} else {
		inspect.IPAM.Driver = "default"
	}
	result := inspectResult(inspect)
	if fake.networks == nil {
		fake.networks = map[string]client.NetworkInspectResult{}
	}
	fake.networks[name] = result
	fake.networks[id] = result
	return client.NetworkCreateResult{ID: id}, nil
}

func (fake *fakeNetworkClient) NetworkRemove(_ context.Context, name string, _ client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	fake.removed = append(fake.removed, name)
	result, found := fake.networks[name]
	if found {
		delete(fake.networks, name)
		delete(fake.networks, result.Network.ID)
		delete(fake.networks, result.Network.Name)
	}
	return client.NetworkRemoveResult{}, nil
}

func (fake *fakeNetworkClient) NetworkConnect(_ context.Context, _ string, options client.NetworkConnectOptions) (client.NetworkConnectResult, error) {
	fake.connected = append(fake.connected, options.Container)
	return client.NetworkConnectResult{}, nil
}

func (fake *fakeNetworkClient) NetworkDisconnect(_ context.Context, _ string, options client.NetworkDisconnectOptions) (client.NetworkDisconnectResult, error) {
	fake.disconnected = append(fake.disconnected, options.Container)
	return client.NetworkDisconnectResult{}, nil
}

func (fake *fakeNetworkClient) ContainerInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if fake.containers[name] {
		return client.ContainerInspectResult{Container: containertypes.InspectResponse{ID: name, Name: "/" + name}}, nil
	}
	return client.ContainerInspectResult{}, errdefs.ErrNotFound.WithMessage("container not found")
}

func (fake *fakeNetworkClient) Close() error {
	fake.closed = true
	return nil
}

func inspectResult(inspect network.Inspect) client.NetworkInspectResult {
	raw, _ := json.Marshal(inspect)
	return client.NetworkInspectResult{Network: inspect, Raw: raw}
}

func seedNetwork(fake *fakeNetworkClient, inspect network.Inspect) {
	if fake.networks == nil {
		fake.networks = map[string]client.NetworkInspectResult{}
	}
	result := inspectResult(inspect)
	fake.networks[inspect.Name] = result
	if inspect.ID != "" {
		fake.networks[inspect.ID] = result
	}
}

func networkDependencies(fake *fakeNetworkClient) docker.Dependencies {
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	}
}

func boolPtr(value bool) *bool { return &value }

func TestCheckModeCreateDoesNotCallNetworkCreate(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependenciesAndState(Request{Name: "web"}, networkDependencies(fake), execution.State{CheckMode: true})
	if response.Failed || !response.Changed || len(fake.created) != 0 || len(fake.removed) != 0 {
		t.Fatalf("response = %#v created=%v removed=%v", response, fake.created, fake.removed)
	}
	if response.Network != nil {
		t.Fatalf("check-mode create network = %#v, want null", response.Network)
	}
	if len(response.Actions) == 0 || !strings.Contains(response.Actions[0], "Created network web") {
		t.Fatalf("actions = %#v", response.Actions)
	}
}

func TestIdempotentPresentDoesNotRecreate(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Scope = "local"
	inspect.EnableIPv4 = true
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{Name: "web"}, networkDependencies(fake))
	if response.Failed || response.Changed || len(fake.created) != 0 || len(fake.removed) != 0 {
		t.Fatalf("response = %#v created=%v removed=%v", response, fake.created, fake.removed)
	}
	if response.Network["Id"] != "abc" || response.Network["Name"] != "web" {
		t.Fatalf("network = %#v", response.Network)
	}
}

func TestRecreateOnDriverChangeWithoutForce(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "old"
	inspect.Name = "web"
	inspect.Driver = "overlay"
	inspect.Scope = "swarm"
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{Name: "web", Driver: "bridge"}, networkDependencies(fake))
	if response.Failed || !response.Changed || len(fake.removed) != 1 || len(fake.created) != 1 {
		t.Fatalf("response = %#v created=%v removed=%v", response, fake.created, fake.removed)
	}
	if fake.createOpts[0].Driver != "bridge" {
		t.Fatalf("create opts = %#v", fake.createOpts[0])
	}
}

func TestRecreateOnInternalAndIPAMChange(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "old"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Internal = true
	inspect.IPAM = network.IPAM{Driver: "default", Config: []network.IPAMConfig{{
		Subnet:  netip.MustParsePrefix("10.25.120.0/24"),
		Gateway: netip.MustParseAddr("10.25.120.2"),
	}}}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Internal: boolPtr(false),
		IPAMConfig: []IPAMConfig{{
			Subnet:  "10.25.121.0/24",
			Gateway: "10.25.121.2",
		}},
	}, networkDependencies(fake))
	if response.Failed || !response.Changed || len(fake.created) != 1 {
		t.Fatalf("response = %#v created=%v", response, fake.created)
	}
}

func TestIPAMConfigIdempotentWithNormalizedAddresses(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.EnableIPv6 = true
	inspect.IPAM = network.IPAM{Driver: "default", Config: []network.IPAMConfig{{
		Subnet:  netip.MustParsePrefix("2001:db8::/64"),
		Gateway: netip.MustParseAddr("2001:db8::1"),
		AuxAddress: map[string]netip.Addr{
			"router": netip.MustParseAddr("2001:db8::2"),
		},
	}}}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{
		Name:       "web",
		EnableIPv6: boolPtr(true),
		IPAMConfig: []IPAMConfig{{
			Subnet:       "2001:0db8:0000::/64",
			Gateway:      "2001:0db8::1",
			AuxAddresses: map[string]string{"router": "2001:0db8:0:0::2"},
		}},
	}, networkDependencies(fake))
	if response.Failed || response.Changed || len(fake.created) != 0 {
		t.Fatalf("response = %#v created=%v", response, fake.created)
	}
}

func TestIPAMSubsetDoesNotRecreateWhenGatewayOmitted(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.IPAM = network.IPAM{Driver: "default", Config: []network.IPAMConfig{{
		Subnet:  netip.MustParsePrefix("10.25.121.0/24"),
		Gateway: netip.MustParseAddr("10.25.121.2"),
		IPRange: netip.MustParsePrefix("10.25.121.0/26"),
	}}}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{
		Name:       "web",
		IPAMConfig: []IPAMConfig{{Subnet: "10.25.121.0/24"}},
	}, networkDependencies(fake))
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
}

func TestConnectedReplaceAndAppends(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Containers = map[string]network.EndpointResource{
		"id-c1": {Name: "c1"},
	}
	fake := &fakeNetworkClient{containers: map[string]bool{"c1": true, "c2": true, "c3": true}}
	seedNetwork(fake, inspect)

	appended := ExecuteWithDependencies(Request{
		Name:      "web",
		Appends:   true,
		Connected: ContainerNames{"c3"},
	}, networkDependencies(fake))
	if appended.Failed || !appended.Changed || strings.Join(fake.connected, ",") != "c3" || len(fake.disconnected) != 0 {
		t.Fatalf("appends = %#v connected=%v disconnected=%v", appended, fake.connected, fake.disconnected)
	}

	fake.connected = nil
	fake.disconnected = nil
	replaced := ExecuteWithDependencies(Request{
		Name:      "web",
		Connected: ContainerNames{"c2"},
	}, networkDependencies(fake))
	if replaced.Failed || !replaced.Changed || strings.Join(fake.connected, ",") != "c2" || strings.Join(fake.disconnected, ",") != "c1" {
		t.Fatalf("replace = %#v connected=%v disconnected=%v", replaced, fake.connected, fake.disconnected)
	}
}

func TestOmittedConnectedDoesNotDisconnectExisting(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Containers = map[string]network.EndpointResource{
		"id-c1": {Name: "c1"},
	}
	fake := &fakeNetworkClient{containers: map[string]bool{"c1": true}}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{Name: "web"}, networkDependencies(fake))
	if response.Failed || response.Changed || len(fake.disconnected) != 0 {
		t.Fatalf("response = %#v disconnected=%v", response, fake.disconnected)
	}
}

func TestAbsentDisconnectsThenRemoves(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Containers = map[string]network.EndpointResource{
		"id-c1": {Name: "c1"},
	}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{Name: "web", State: "absent"}, networkDependencies(fake))
	if response.Failed || !response.Changed || strings.Join(fake.disconnected, ",") != "c1" || strings.Join(fake.removed, ",") != "web" {
		t.Fatalf("response = %#v disconnected=%v removed=%v", response, fake.disconnected, fake.removed)
	}
	if response.Network != nil {
		t.Fatalf("absent network = %#v", response.Network)
	}
}

func TestAbsentMissingIsIdempotent(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependencies(Request{Name: "missing", State: "absent"}, networkDependencies(fake))
	if response.Failed || response.Changed || len(fake.removed) != 0 {
		t.Fatalf("response = %#v removed=%v", response, fake.removed)
	}
}

func TestCIDRValidation(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependencies(Request{
		Name:       "web",
		IPAMConfig: []IPAMConfig{{Subnet: "fdd1:ac8c:0557:7ce1::"}},
	}, networkDependencies(fake))
	if !response.Failed || response.Msg != `"fdd1:ac8c:0557:7ce1::" is not a valid CIDR` || len(fake.created) != 0 {
		t.Fatalf("response = %#v", response)
	}
}

func TestBooleanDriverOptionsAreStringified(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Options = map[string]string{"com.docker.network.bridge.enable_icc": "false"}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{
		Name: "web",
		DriverOptions: map[string]any{
			"com.docker.network.bridge.enable_icc": false,
		},
	}, networkDependencies(fake))
	if response.Failed || response.Changed {
		t.Fatalf("boolean stringification was not idempotent: %#v", response)
	}

	changed := ExecuteWithDependencies(Request{
		Name: "web",
		DriverOptions: map[string]any{
			"com.docker.network.bridge.enable_icc": true,
		},
	}, networkDependencies(fake))
	if changed.Failed || !changed.Changed || len(fake.created) != 1 {
		t.Fatalf("boolean change = %#v created=%v", changed, fake.created)
	}
	if fake.createOpts[0].Options["com.docker.network.bridge.enable_icc"] != "true" {
		t.Fatalf("create options = %#v", fake.createOpts[0].Options)
	}
}

func TestForceRecreatesEvenWhenUnchanged(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependencies(Request{Name: "web", Force: true}, networkDependencies(fake))
	if response.Failed || !response.Changed || len(fake.removed) != 1 || len(fake.created) != 1 {
		t.Fatalf("response = %#v created=%v removed=%v", response, fake.created, fake.removed)
	}
}

func TestDiffModeReportsBeforeAfterWithoutDifferencesList(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "overlay"
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	response := ExecuteWithDependenciesAndState(Request{Name: "web", Driver: "bridge"}, networkDependencies(fake), execution.State{DiffMode: true})
	if response.Failed || response.Diff == nil {
		t.Fatalf("response = %#v", response)
	}
	if response.Diff.Before["driver"] != "overlay" || response.Diff.After["driver"] != "bridge" {
		t.Fatalf("diff = %#v", response.Diff)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"differences"`) {
		t.Fatalf("diff JSON included differences list: %s", encoded)
	}
}

func TestConnectedObjectFormExtractsName(t *testing.T) {
	var names ContainerNames
	if err := json.Unmarshal([]byte(`[{"name":"c1","ipv4_address":"10.0.0.2"},"c2"]`), &names); err != nil {
		t.Fatal(err)
	}
	if strings.Join([]string(names), ",") != "c1,c2" {
		t.Fatalf("names = %#v", names)
	}
}

func TestIPAMConfigAcceptsLegacyKeys(t *testing.T) {
	var config IPAMConfig
	if err := json.Unmarshal([]byte(`{"subnet":"10.0.0.0/24","ip_range":"10.0.0.0/26","aux_address":{"host1":"10.0.0.3"}}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.IPRange != "10.0.0.0/26" || config.AuxAddresses["host1"] != "10.0.0.3" {
		t.Fatalf("config = %#v", config)
	}
}

func TestOptionsAliasPopulatesDriverOptions(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependencies(Request{
		Name:    "web",
		Options: map[string]any{"com.docker.network.bridge.enable_icc": "false"},
	}, networkDependencies(fake))
	if response.Failed || !response.Changed || fake.createOpts[0].Options["com.docker.network.bridge.enable_icc"] != "false" {
		t.Fatalf("response = %#v opts=%#v", response, fake.createOpts)
	}
}

func TestConfigOnlyForcesNullDriver(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependencies(Request{
		Name:       "web",
		Driver:     "bridge",
		ConfigOnly: boolPtr(true),
	}, networkDependencies(fake))
	if response.Failed || fake.createOpts[0].Driver != "null" || !fake.createOpts[0].ConfigOnly {
		t.Fatalf("response = %#v opts=%#v", response, fake.createOpts)
	}
}

func TestSwarmScopeRemovalSleepsUntilGone(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "overlay"
	inspect.Scope = "swarm"
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	clock := &recordingClock{}
	deps := docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		Clock:       clock,
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return &swarmRemoveClient{fakeNetworkClient: fake, remaining: 1}, nil
		},
	}
	response := ExecuteWithDependencies(Request{Name: "web", State: "absent"}, deps)
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 100*time.Millisecond {
		t.Fatalf("sleeps = %#v", clock.sleeps)
	}
}

type swarmRemoveClient struct {
	*fakeNetworkClient
	remaining int
}

func (fake *swarmRemoveClient) NetworkRemove(ctx context.Context, name string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	return fake.fakeNetworkClient.NetworkRemove(ctx, name, options)
}

func (fake *swarmRemoveClient) NetworkInspect(ctx context.Context, name string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	if fake.remaining > 0 && len(fake.removed) > 0 {
		fake.remaining--
		inspect := network.Inspect{}
		inspect.Name = name
		inspect.Scope = "swarm"
		return inspectResult(inspect), nil
	}
	return fake.fakeNetworkClient.NetworkInspect(ctx, name, options)
}

type recordingClock struct {
	sleeps []time.Duration
}

func (clock *recordingClock) Now() time.Time { return time.Time{} }

func (clock *recordingClock) Sleep(delay time.Duration) {
	clock.sleeps = append(clock.sleeps, delay)
}

func TestLabelsAreComparedAsSubset(t *testing.T) {
	inspect := network.Inspect{}
	inspect.ID = "abc"
	inspect.Name = "web"
	inspect.Driver = "bridge"
	inspect.Labels = map[string]string{"a": "1", "b": "2"}
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	less := ExecuteWithDependencies(Request{Name: "web", Labels: map[string]string{"a": "1"}}, networkDependencies(fake))
	if less.Failed || less.Changed {
		t.Fatalf("subset labels recreated: %#v", less)
	}
	more := ExecuteWithDependencies(Request{Name: "web", Labels: map[string]string{"a": "1", "c": "3"}}, networkDependencies(fake))
	if more.Failed || !more.Changed {
		t.Fatalf("additional labels = %#v", more)
	}
}

func TestEnableIPv4PointerIsForwardedOnCreate(t *testing.T) {
	fake := &fakeNetworkClient{networks: map[string]client.NetworkInspectResult{}}
	response := ExecuteWithDependencies(Request{Name: "web", EnableIPv4: boolPtr(false)}, networkDependencies(fake))
	if response.Failed || fake.createOpts[0].EnableIPv4 == nil || *fake.createOpts[0].EnableIPv4 {
		t.Fatalf("response = %#v opts=%#v", response, fake.createOpts)
	}
}

func TestMissingNameRejectedBeforeClient(t *testing.T) {
	created := false
	response := ExecuteWithDependencies(Request{}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			created = true
			return &fakeNetworkClient{}, nil
		},
	})
	if !response.Failed || response.Msg != "name is required" || created {
		t.Fatalf("response = %#v created=%t", response, created)
	}
}

func TestInspectUsesVerbose(t *testing.T) {
	inspect := network.Inspect{}
	inspect.Name = "web"
	inspect.Driver = "bridge"
	fake := &fakeNetworkClient{}
	seedNetwork(fake, inspect)
	_ = ExecuteWithDependencies(Request{Name: "web"}, networkDependencies(fake))
	if len(fake.verbose) == 0 || !fake.verbose[0] {
		t.Fatalf("verbose = %#v", fake.verbose)
	}
}
