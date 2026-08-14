package docker_swarm_service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/go-digest"
)

type fakeClock struct{ sleeps int }

func (clock *fakeClock) Now() time.Time { return time.Unix(1, 234) }
func (clock *fakeClock) Sleep(time.Duration) {
	clock.sleeps++
}

type serviceRecord struct {
	service swarm.Service
}

type serviceClient struct {
	client.APIClient
	services        map[string]serviceRecord
	networks        []network.Summary
	inspectNetworks []network.Network
	configs         []swarm.Config
	secrets         []swarm.Secret
	digest          string
	creates         int
	updates         int
	removes         int
	updateErr       string
	inspectCalls    int
}

func (fake *serviceClient) Close() error { return nil }

func (fake *serviceClient) ServiceInspect(_ context.Context, name string, _ client.ServiceInspectOptions) (client.ServiceInspectResult, error) {
	fake.inspectCalls++
	record, found := fake.services[name]
	if !found {
		for _, item := range fake.services {
			if item.service.ID == name {
				return client.ServiceInspectResult{Service: item.service}, nil
			}
		}
		return client.ServiceInspectResult{}, errdefs.ErrNotFound
	}
	return client.ServiceInspectResult{Service: record.service}, nil
}

func (fake *serviceClient) ServiceCreate(_ context.Context, options client.ServiceCreateOptions) (client.ServiceCreateResult, error) {
	fake.creates++
	id := "svc-" + options.Spec.Name
	service := swarm.Service{
		ID:   id,
		Meta: swarm.Meta{Version: swarm.Version{Index: 1}},
		Spec: options.Spec,
	}
	fake.services[options.Spec.Name] = serviceRecord{service: service}
	return client.ServiceCreateResult{ID: id}, nil
}

func (fake *serviceClient) ServiceUpdate(_ context.Context, id string, options client.ServiceUpdateOptions) (client.ServiceUpdateResult, error) {
	fake.updates++
	if fake.updateErr != "" {
		return client.ServiceUpdateResult{}, errors.New(fake.updateErr)
	}
	for name, record := range fake.services {
		if record.service.ID == id || record.service.Spec.Name == id {
			record.service.Spec = options.Spec
			record.service.Version.Index++
			fake.services[name] = record
			break
		}
	}
	return client.ServiceUpdateResult{}, nil
}

func (fake *serviceClient) ServiceRemove(_ context.Context, id string, _ client.ServiceRemoveOptions) (client.ServiceRemoveResult, error) {
	fake.removes++
	delete(fake.services, id)
	for name, record := range fake.services {
		if record.service.ID == id {
			delete(fake.services, name)
		}
	}
	return client.ServiceRemoveResult{}, nil
}

func (fake *serviceClient) NetworkList(context.Context, client.NetworkListOptions) (client.NetworkListResult, error) {
	return client.NetworkListResult{Items: fake.networks}, nil
}

func (fake *serviceClient) NetworkInspect(_ context.Context, id string, _ client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	for _, item := range fake.networks {
		if item.ID == id || item.Name == id {
			return client.NetworkInspectResult{Network: network.Inspect{Network: item.Network}}, nil
		}
	}
	for _, item := range fake.inspectNetworks {
		if item.ID == id || item.Name == id {
			return client.NetworkInspectResult{Network: network.Inspect{Network: item}}, nil
		}
	}
	return client.NetworkInspectResult{}, errdefs.ErrNotFound
}

func (fake *serviceClient) ConfigList(context.Context, client.ConfigListOptions) (client.ConfigListResult, error) {
	return client.ConfigListResult{Items: fake.configs}, nil
}

func (fake *serviceClient) SecretList(context.Context, client.SecretListOptions) (client.SecretListResult, error) {
	return client.SecretListResult{Items: fake.secrets}, nil
}

func (fake *serviceClient) DistributionInspect(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
	digestValue := fake.digest
	if digestValue == "" {
		digestValue = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	return client.DistributionInspectResult{
		DistributionInspect: registry.DistributionInspect{
			Descriptor: ocispec.Descriptor{Digest: digest.Digest(digestValue)},
		},
	}, nil
}

func serviceDependencies(fake *serviceClient, clock docker.Clock) docker.Dependencies {
	if fake.services == nil {
		fake.services = map[string]serviceRecord{}
	}
	if clock == nil {
		clock = &fakeClock{}
	}
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		Clock:       clock,
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
	}
}

func existingService(name, image string, replicas uint64) serviceRecord {
	copyReplicas := replicas
	return serviceRecord{service: swarm.Service{
		ID:   "id-" + name,
		Meta: swarm.Meta{Version: swarm.Version{Index: 1}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: image, Command: []string{"sleep", "3000"}},
			},
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &copyReplicas}},
		},
	}}
}

func TestMissingNameFails(t *testing.T) {
	response := Execute(Request{})
	if !response.Failed || response.Msg != "missing required arguments: name" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAbsentMissingIsUnchanged(t *testing.T) {
	fake := &serviceClient{}
	response := ExecuteWithDependencies(Request{Name: "missing", State: "absent"}, serviceDependencies(fake, nil))
	if response.Failed || response.Changed || response.Msg != "Service absent" {
		t.Fatalf("response = %#v", response)
	}
}

func TestCreateUpdateIdempotentAndAbsent(t *testing.T) {
	fake := &serviceClient{}
	created := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(1),
		Command:  []string{"sleep", "3000"},
	}, serviceDependencies(fake, nil))
	if created.Failed || !created.Changed || created.Msg != "Service created" || fake.creates != 1 {
		t.Fatalf("create = %#v creates=%d", created, fake.creates)
	}
	if created.SwarmService["image"] != "alpine:latest" {
		t.Fatalf("facts = %#v", created.SwarmService)
	}

	idem := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(1),
		Command:  []string{"sleep", "3000"},
	}, serviceDependencies(fake, nil))
	if idem.Failed || idem.Changed || idem.Msg != "Service unchanged" {
		t.Fatalf("idem = %#v", idem)
	}

	scaled := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(2),
		Command:  []string{"sleep", "3000"},
	}, serviceDependencies(fake, nil))
	if scaled.Failed || !scaled.Changed || scaled.Msg != "Service updated" || fake.updates != 1 {
		t.Fatalf("scale = %#v updates=%d", scaled, fake.updates)
	}

	removed := ExecuteWithDependencies(Request{Name: "web", State: "absent"}, serviceDependencies(fake, nil))
	if removed.Failed || !removed.Changed || removed.Msg != "Service removed" {
		t.Fatalf("remove = %#v", removed)
	}
}

func TestCheckModeDoesNotMutate(t *testing.T) {
	fake := &serviceClient{}
	response := ExecuteWithDependenciesAndState(Request{
		Name:  "web",
		Image: "alpine:latest",
	}, serviceDependencies(fake, nil), execution.State{CheckMode: true, DiffMode: true})
	if response.Failed || !response.Changed || fake.creates != 0 || response.Diff == nil {
		t.Fatalf("response = %#v creates=%d", response, fake.creates)
	}
}

func TestModeChangeRebuilds(t *testing.T) {
	fake := &serviceClient{services: map[string]serviceRecord{
		"web": existingService("web", "alpine:latest", 1),
	}}
	response := ExecuteWithDependencies(Request{
		Name:  "web",
		Image: "alpine:latest",
		Mode:  "global",
	}, serviceDependencies(fake, nil))
	if response.Failed || !response.Changed || !response.Rebuilt || fake.removes != 1 || fake.creates != 1 {
		t.Fatalf("response = %#v removes=%d creates=%d", response, fake.removes, fake.creates)
	}
}

func TestForceUpdateMessage(t *testing.T) {
	fake := &serviceClient{services: map[string]serviceRecord{
		"web": existingService("web", "alpine:latest", 1),
	}}
	response := ExecuteWithDependencies(Request{
		Name:        "web",
		Image:       "alpine:latest",
		ForceUpdate: true,
		Command:     []string{"sleep", "3000"},
		Replicas:    int64Ptr(1),
	}, serviceDependencies(fake, nil))
	if response.Failed || !response.Changed || response.Msg != "Service forcefully updated" {
		t.Fatalf("response = %#v", response)
	}
}

func TestResolveImagePinsDigest(t *testing.T) {
	fake := &serviceClient{}
	resolve := true
	created := ExecuteWithDependencies(Request{
		Name:         "web",
		Image:        "alpine:latest",
		ResolveImage: &resolve,
	}, serviceDependencies(fake, nil))
	if created.Failed || created.SwarmService["image"] != "alpine:latest@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("created = %#v", created.SwarmService)
	}
}

func TestResolveImageError(t *testing.T) {
	failing := &failingDistributionClient{}
	resolve := true
	response := ExecuteWithDependencies(Request{
		Name:         "web",
		Image:        "missing:latest",
		ResolveImage: &resolve,
	}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		Clock:       &fakeClock{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return failing, nil
		},
	})
	if !response.Failed || !strings.Contains(response.Msg, "Error looking for an image named missing:latest") {
		t.Fatalf("response = %#v", response)
	}
}

func TestOmittedEnvIsPreservedOnUpdate(t *testing.T) {
	replicas := uint64(1)
	fake := &serviceClient{services: map[string]serviceRecord{
		"web": {service: swarm.Service{
			ID:   "id-web",
			Meta: swarm.Meta{Version: swarm.Version{Index: 1}},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image:   "alpine:latest",
						Command: []string{"sleep", "3000"},
						Env:     []string{"FOO=bar"},
					},
				},
				Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			},
		}},
	}}
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(2),
		Command:  []string{"sleep", "3000"},
	}, serviceDependencies(fake, nil))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	got := fake.services["web"].service.Spec.TaskTemplate.ContainerSpec.Env
	if len(got) != 1 || got[0] != "FOO=bar" {
		t.Fatalf("env was not preserved: %#v", got)
	}
}

func TestEmptyHealthcheckClears(t *testing.T) {
	replicas := uint64(1)
	fake := &serviceClient{services: map[string]serviceRecord{
		"web": {service: swarm.Service{
			ID:   "id-web",
			Meta: swarm.Meta{Version: swarm.Version{Index: 1}},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image: "alpine:latest",
						Healthcheck: &container.HealthConfig{
							Test: []string{"CMD", "true"},
						},
					},
				},
				Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
			},
		}},
	}}
	cleared := ExecuteWithDependencies(Request{
		Name:        "web",
		Image:       "alpine:latest",
		Healthcheck: &Healthcheck{},
	}, serviceDependencies(fake, nil))
	if cleared.Failed || !cleared.Changed {
		t.Fatalf("clear = %#v", cleared)
	}
	if got := fake.services["web"].service.Spec.TaskTemplate.ContainerSpec.Healthcheck; got != nil {
		t.Fatalf("healthcheck was not cleared: %#v", got)
	}
	again := ExecuteWithDependencies(Request{
		Name:        "web",
		Image:       "alpine:latest",
		Healthcheck: &Healthcheck{},
	}, serviceDependencies(fake, nil))
	if again.Failed || again.Changed {
		t.Fatalf("empty idem = %#v", again)
	}
}

func TestPrefersSwarmScopedHostNetwork(t *testing.T) {
	fake := &serviceClient{
		networks: []network.Summary{
			{Network: network.Network{Name: "host", ID: "local-host", Scope: "local"}},
			{Network: network.Network{Name: "host", ID: "swarm-host", Scope: "swarm"}},
		},
	}
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Networks: NetworkList{{Name: "host"}},
	}, serviceDependencies(fake, nil))
	if response.Failed {
		t.Fatalf("response = %#v", response)
	}
	networks := fake.services["web"].service.Spec.TaskTemplate.Networks
	if len(networks) != 1 || networks[0].Target != "swarm-host" {
		t.Fatalf("networks = %#v", networks)
	}
}

func TestHostNetworkIdempotentAcrossLocalAndSwarmIDs(t *testing.T) {
	replicas := uint64(1)
	fake := &serviceClient{
		networks: []network.Summary{
			{Network: network.Network{Name: "host", ID: "local-host", Scope: "local"}},
		},
		inspectNetworks: []network.Network{
			{Name: "host", ID: "swarm-host", Scope: "swarm"},
		},
		services: map[string]serviceRecord{
			"web": {service: swarm.Service{
				ID: "svc-web",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "web"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{Image: "alpine:latest"},
						Networks:      []swarm.NetworkAttachmentConfig{{Target: "swarm-host"}},
					},
					Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
				},
			}},
		},
	}
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Networks: NetworkList{{Name: "host"}},
	}, serviceDependencies(fake, nil))
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}
	if fake.updates != 0 {
		t.Fatalf("updates = %d", fake.updates)
	}
}

func TestOmittedNetworksDoNotClearExisting(t *testing.T) {
	replicas := uint64(1)
	source := "data"
	fake := &serviceClient{
		networks: []network.Summary{
			{Network: network.Network{Name: "app", ID: "net-app", Scope: "swarm"}},
		},
		services: map[string]serviceRecord{
			"web": {service: swarm.Service{
				ID: "svc-web",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "web"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{Image: "alpine:latest"},
						Networks:      []swarm.NetworkAttachmentConfig{{Target: "net-app"}},
					},
					Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
				},
			}},
		},
	}
	response := ExecuteWithDependencies(Request{
		Name:   "web",
		Image:  "alpine:latest",
		Mounts: []MountSpec{{Source: &source, Target: "/tmp/data", Type: "volume"}},
	}, serviceDependencies(fake, nil))
	if response.Failed || !response.Changed {
		t.Fatalf("response = %#v", response)
	}
	for _, change := range response.Changes {
		if change == "networks" {
			t.Fatalf("omitted networks were compared: %#v", response)
		}
	}
	networks := fake.services["web"].service.Spec.TaskTemplate.Networks
	if len(networks) != 1 || networks[0].Target != "net-app" {
		t.Fatalf("networks = %#v", networks)
	}
}

func TestCapabilityCasingIsNormalized(t *testing.T) {
	replicas := uint64(1)
	fake := &serviceClient{
		services: map[string]serviceRecord{
			"web": {service: swarm.Service{
				ID: "svc-web",
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "web"},
					TaskTemplate: swarm.TaskSpec{
						ContainerSpec: &swarm.ContainerSpec{
							Image:           "alpine:latest",
							CapabilityAdd:   []string{"CAP_SYS_TIME"},
							CapabilityDrop:  []string{"ALL"},
							Init:            boolPtr(true),
						},
					},
					Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
				},
			}},
		},
	}
	init := true
	response := ExecuteWithDependencies(Request{
		Name:    "web",
		Image:   "alpine:latest",
		Init:    &init,
		CapAdd:  []string{"sys_time"},
		CapDrop: []string{"all"},
	}, serviceDependencies(fake, nil))
	if response.Failed || response.Changed {
		t.Fatalf("response = %#v", response)
	}

	createFake := &serviceClient{}
	created := ExecuteWithDependencies(Request{
		Name:    "caps",
		Image:   "alpine:latest",
		Init:    &init,
		CapAdd:  []string{"sys_time"},
		CapDrop: []string{"all"},
	}, serviceDependencies(createFake, nil))
	if created.Failed || !created.Changed {
		t.Fatalf("create = %#v", created)
	}
	spec := createFake.services["caps"].service.Spec.TaskTemplate.ContainerSpec
	if len(spec.CapabilityAdd) != 1 || spec.CapabilityAdd[0] != "CAP_SYS_TIME" {
		t.Fatalf("cap_add = %#v", spec.CapabilityAdd)
	}
	if len(spec.CapabilityDrop) != 1 || spec.CapabilityDrop[0] != "ALL" {
		t.Fatalf("cap_drop = %#v", spec.CapabilityDrop)
	}
}

func TestMissingSecretFails(t *testing.T) {
	fake := &serviceClient{}
	response := ExecuteWithDependencies(Request{
		Name:    "web",
		Image:   "alpine:latest",
		Secrets: []FileReference{{SecretName: "missing-secret"}},
	}, serviceDependencies(fake, nil))
	if !response.Failed || !strings.Contains(response.Msg, `Could not find a secret named "missing-secret"`) {
		t.Fatalf("response = %#v", response)
	}
}

type failingDistributionClient struct{ client.APIClient }

func (*failingDistributionClient) Close() error { return nil }

func (*failingDistributionClient) DistributionInspect(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
	return client.DistributionInspectResult{}, errors.New("not found")
}

func (*failingDistributionClient) ServiceInspect(context.Context, string, client.ServiceInspectOptions) (client.ServiceInspectResult, error) {
	return client.ServiceInspectResult{}, errdefs.ErrNotFound
}
