package docker_container

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	mounttypes "github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type fakeContainerClient struct {
	client.APIClient
	inspect    container.InspectResponse
	inspectErr error
	imageErr   error
	removeErr  error
	removed    int
	logsCalled int
	waitStatus int64
	closed     bool
}

func (fake *fakeContainerClient) ContainerInspect(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{Container: fake.inspect}, fake.inspectErr
}

type recordingFileSystem struct {
	docker.FileSystem
	home string
	abs  map[string]string
}

func (fileSystem recordingFileSystem) UserHomeDir() (string, error) { return fileSystem.home, nil }
func (fileSystem recordingFileSystem) Abs(path string) (string, error) {
	if value, found := fileSystem.abs[path]; found {
		return value, nil
	}
	return "", errors.New("unexpected relative path")
}

type advancingClock struct {
	docker.Clock
	now time.Time
}

func (clock *advancingClock) Now() time.Time { return clock.now }
func (clock *advancingClock) Sleep(delay time.Duration) {
	clock.now = clock.now.Add(delay)
}

func (fake *fakeContainerClient) ContainerRemove(_ context.Context, _ string, _ client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	fake.removed++
	return client.ContainerRemoveResult{}, fake.removeErr
}

func (fake *fakeContainerClient) ContainerWait(_ context.Context, _ string, _ client.ContainerWaitOptions) client.ContainerWaitResult {
	result := make(chan container.WaitResponse, 1)
	errors := make(chan error, 1)
	result <- container.WaitResponse{StatusCode: fake.waitStatus}
	return client.ContainerWaitResult{Result: result, Error: errors}
}

func (fake *fakeContainerClient) ContainerLogs(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	fake.logsCalled++
	return nil, errors.New("logs should not be requested")
}

func (fake *fakeContainerClient) ImageInspect(_ context.Context, reference string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: reference}}, fake.imageErr
}

func (fake *fakeContainerClient) Close() error {
	fake.closed = true
	return nil
}

func TestCheckModeReportsAbsentWithoutMutation(t *testing.T) {
	fake := &fakeContainerClient{inspect: container.InspectResponse{
		ID: "container-id", Name: "/web",
		State:      &container.State{Status: container.StateRunning, Running: true},
		Config:     &container.Config{Image: "alpine:latest"},
		HostConfig: &container.HostConfig{},
	}}
	response := ExecuteWithDependenciesAndState(Request{Name: "web", State: "absent"}, docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient:   func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
	}, execution.State{CheckMode: true, DiffMode: true})

	if response.Failed || !response.Changed {
		t.Fatalf("check-mode response = %#v", response)
	}
	if fake.removed != 0 {
		t.Fatalf("check mode removed %d containers", fake.removed)
	}
	if !fake.closed {
		t.Fatal("injected client was not closed")
	}
	wantActions := []map[string]any{
		{"stopped": "container-id", "timeout": (*int)(nil)},
		{"removed": "container-id", "volume_state": false, "link": false, "force": false},
	}
	if response.Diff == nil || !reflect.DeepEqual(response.Actions, wantActions) {
		t.Fatalf("check-mode evidence = %#v", response)
	}
}

func TestNormalizeDefaultsPreservesOmittedValues(t *testing.T) {
	request := normalizeDefaults(Request{})
	if request.State != "started" || request.ContainerDefaultBehavior != "no_defaults" || request.Detach != nil || request.Privileged != nil {
		t.Fatalf("no-defaults request = %#v", request)
	}
	compatibility := normalizeDefaults(Request{ContainerDefaultBehavior: "compatibility"})
	if compatibility.Detach == nil || !*compatibility.Detach || compatibility.Privileged == nil || *compatibility.Privileged {
		t.Fatalf("compatibility defaults = %#v", compatibility)
	}
	commandCompatibility := normalizeDefaults(Request{CommandHandling: "compatibility", Entrypoint: []string{}})
	if commandCompatibility.Entrypoint != nil {
		t.Fatalf("compatibility empty entrypoint = %#v", commandCompatibility.Entrypoint)
	}
}

func TestBuildContainerConfigMapsCanonicalOptions(t *testing.T) {
	falseValue := false
	trueValue := true
	weight := int64(500)
	period := int64(100000)
	quota := int64(50000)
	shares := int64(256)
	cpus := 1.5
	memory := "64m"
	reservation := "32m"
	swap := "128m"
	pids := int64(64)
	retries := 4
	req := normalizeDefaults(Request{
		Name: "web", Image: "alpine:latest", Command: `echo "hello world"`,
		Capabilities: []string{"NET_ADMIN"}, CapDrop: []string{"MKNOD"},
		AutoRemove: &falseValue, Init: &trueValue, Privileged: &falseValue, ReadOnly: &trueValue, TTY: &trueValue,
		BlkioWeight: &weight, CPUPeriod: &period, CPUQuota: &quota, CPUShares: &shares, CPUs: &cpus,
		Memory: &memory, MemoryReservation: &reservation, MemorySwap: &swap, PidsLimit: &pids,
		RestartPolicy: "on-failure", RestartRetries: &retries,
		PublishedPorts: []string{"127.0.0.1:48080:80"}, ExposedPorts: []string{"81-82/udp"},
		Ulimits: []string{"nofile:1024:2048"}, SecurityOptions: []string{"no-new-privileges:true"},
	})
	config, host, err := buildContainerConfig(req, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Cmd, []string{"echo", "hello world"}) {
		t.Fatalf("command = %#v", config.Cmd)
	}
	if host.BlkioWeight != 500 || host.CPUPeriod != period || host.CPUQuota != quota || host.CPUShares != shares || host.NanoCPUs != 1500000000 {
		t.Fatalf("resources = %#v", host.Resources)
	}
	if host.Memory != 64*1024*1024 || host.MemoryReservation != 32*1024*1024 || host.MemorySwap != 128*1024*1024 || host.PidsLimit == nil || *host.PidsLimit != pids {
		t.Fatalf("memory resources = %#v", host.Resources)
	}
	if host.RestartPolicy.Name != container.RestartPolicyOnFailure || host.RestartPolicy.MaximumRetryCount != retries {
		t.Fatalf("restart policy = %#v", host.RestartPolicy)
	}
	if len(host.PortBindings) != 1 || len(config.ExposedPorts) != 3 {
		t.Fatalf("ports = %#v / %#v", host.PortBindings, config.ExposedPorts)
	}
	if len(host.Ulimits) != 1 || host.Ulimits[0].Soft != 1024 || host.Ulimits[0].Hard != 2048 {
		t.Fatalf("ulimits = %#v", host.Ulimits)
	}
}

func TestBuildContainerConfigMapsRemainingPinnedOptions(t *testing.T) {
	falseValue, trueValue := false, true
	swappiness, oomScore, pids := int64(40), 250, int64(32)
	shm := "32m"
	request := normalizeDefaults(Request{
		Name: "web", Image: "alpine:latest", Entrypoint: []string{"/bin/sh", "-c"},
		Env: map[string]string{"A": "one"}, Hostname: "host", Domainname: "example.test", User: "1000:1000", WorkingDir: "/work",
		Labels: map[string]string{"role": "test"}, StopSignal: "SIGTERM", Detach: &falseValue, Interactive: &trueValue,
		CgroupParent: "dibra.slice", CgroupnsMode: "private", CPUSetCPUs: "0", CPUSetMems: "0",
		MemorySwappiness: &swappiness, OOMKiller: &trueValue, OOMScoreAdj: &oomScore, PidsLimit: &pids,
		DeviceCgroupRules: []string{"c 1:3 rwm"}, Devices: []string{"/dev/null:/dev/test-null:r"},
		DeviceRequests: []DeviceRequest{{Driver: "cdi", DeviceIDs: []string{"vendor.com/device=test"}, Capabilities: [][]string{{"gpu"}}, Options: map[string]string{"mode": "test"}}},
		DNSServers:     []string{"1.1.1.1"}, DNSOptions: []string{"ndots:1"}, DNSSearchDomains: []string{"example.test"},
		EtcHosts: map[string]string{"db": "127.0.0.2"}, Groups: []string{"44"}, SecurityOptions: []string{"no-new-privileges:true"},
		StorageOptions: map[string]string{"size": "12m"}, Sysctls: map[string]string{"net.ipv4.ip_forward": "1"},
		IPCMode: "private", PIDMode: "host", UTSMode: "host", UsernsMode: "host", Runtime: "runc",
		LogDriver: "json-file", LogOptions: map[string]string{"max-file": "2"}, ShmSize: &shm,
		Tmpfs: []string{"/run:rw,noexec"}, VolumeDriver: "local", VolumesFrom: []string{"data:ro"},
		Mounts:          []Mount{{Type: "volume", Source: "cache", Target: "/cache", NoCopy: &trueValue, Labels: map[string]string{"scope": "test"}, VolumeDriver: "local", VolumeOptions: map[string]string{"type": "tmpfs"}}},
		PublishAllPorts: &trueValue,
	})
	config, host, err := buildContainerConfig(request, recordingFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Entrypoint, []string{"/bin/sh", "-c"}) || !config.OpenStdin || !config.AttachStdin || !config.AttachStdout || !config.AttachStderr || !config.StdinOnce {
		t.Fatalf("config process options = %#v", config)
	}
	if host.CgroupParent != "dibra.slice" || host.CgroupnsMode != container.CgroupnsModePrivate || host.CpusetCpus != "0" || host.CpusetMems != "0" || host.MemorySwappiness == nil || *host.MemorySwappiness != swappiness || host.OomKillDisable == nil || !*host.OomKillDisable || host.OomScoreAdj != oomScore || host.PidsLimit == nil || *host.PidsLimit != pids {
		t.Fatalf("host resource options = %#v", host.Resources)
	}
	if len(host.Devices) != 1 || len(host.DeviceRequests) != 1 || !reflect.DeepEqual(host.DeviceCgroupRules, []string{"c 1:3 rwm"}) || len(host.DNS) != 1 || !reflect.DeepEqual(host.ExtraHosts, []string{"db:127.0.0.2"}) {
		t.Fatalf("host device/network options = %#v", host)
	}
	if host.LogConfig.Type != "json-file" || host.LogConfig.Config["max-file"] != "2" || host.ShmSize != 32*1024*1024 || host.Tmpfs["/run"] != "rw,noexec" || len(host.Mounts) != 1 || host.Mounts[0].VolumeOptions == nil || host.Mounts[0].VolumeOptions.DriverConfig == nil || !host.PublishAllPorts {
		t.Fatalf("host storage options = %#v", host)
	}
}

func TestCompareContainerDistinguishesOmittedAndExplicitFalse(t *testing.T) {
	existing := container.InspectResponse{
		Config: &container.Config{}, HostConfig: &container.HostConfig{Privileged: true},
	}
	omitted := normalizeDefaults(Request{Name: "web"})
	desiredConfig, desiredHost, err := buildContainerConfig(omitted, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	result := compareContainer(omitted, desiredConfig, desiredHost, existing, docker.NewDiffBuilder())
	if result.recreate {
		t.Fatal("omitted privileged value requested recreation")
	}
	falseValue := false
	explicit := normalizeDefaults(Request{Name: "web", Privileged: &falseValue})
	desiredConfig, desiredHost, err = buildContainerConfig(explicit, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	result = compareContainer(explicit, desiredConfig, desiredHost, existing, docker.NewDiffBuilder())
	if !result.recreate {
		t.Fatal("explicit privileged=false did not request recreation")
	}
}

func TestValidationRejectsUpstreamContractViolations(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{"missing-name", Request{}, "name is required"},
		{"bad-state", Request{Name: "web", State: "running"}, "state must be"},
		{"restart-retries", Request{Name: "web", RestartRetries: intPointer(3)}, "restart_policy is required"},
		{"scalar-comparison", Request{Name: "web", Comparisons: map[string]string{"privileged": "allow_more_present"}}, "is a value"},
		{"duration", Request{Name: "web", Healthcheck: &Healthcheck{Interval: "broken"}}, "cannot parse"},
		{"bad-cgroupns", Request{Name: "web", CgroupnsMode: "shared"}, "cgroupns_mode"},
		{"bad-weight", Request{Name: "web", BlkioWeight: int64Pointer(9)}, "blkio_weight"},
		{"bad-swappiness", Request{Name: "web", MemorySwappiness: int64Pointer(101)}, "memory_swappiness"},
		{"bad-default-ip", Request{Name: "web", DefaultHostIP: stringPointer("host.example")}, "default_host_ip"},
		{"bad-network-ip", Request{Name: "web", Networks: []Network{{Name: "net", IPv4Address: "fd00::1"}}}, "invalid IPv4"},
		{"missing-bind-source", Request{Name: "web", Mounts: []Mount{{Type: "bind", Target: "/data"}}}, "source must be specified"},
		{"mount-option-type", Request{Name: "web", Mounts: []Mount{{Type: "tmpfs", Target: "/data", Propagation: "shared"}}}, "not valid for type"},
		{"duplicate-target", Request{Name: "web", Mounts: []Mount{{Target: "/data"}}, Volumes: []string{"cache:/data"}}, "appears both"},
		{"bad-port", Request{Name: "web", PublishedPorts: []string{"localhost:80:80"}}, "not hostnames"},
		{"bad-ulimit", Request{Name: "web", Ulimits: []string{"nofile"}}, "invalid ulimit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRequest(normalizeDefaults(test.req))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRequest() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAbsentStateSkipsUnrelatedOptionParsing(t *testing.T) {
	request := normalizeDefaults(Request{
		Name: "web", State: "absent", Platform: "invalid/platform/with/too/many/parts",
		PublishedPorts: []string{"definitely-invalid"}, Networks: []Network{{Name: "", IPv4Address: "invalid"}},
		Mounts: []Mount{{Type: "invalid", Target: ""}},
	})
	if err := validateRequest(request); err != nil {
		t.Fatalf("state=absent parsed irrelevant options: %v", err)
	}
}

func TestPresentStateAllowsOmittedImageForExistingContainer(t *testing.T) {
	request := normalizeDefaults(Request{Name: "web", State: "present"})
	if err := validateRequest(request); err != nil {
		t.Fatalf("state=present rejected an image omitted for an existing container: %v", err)
	}
}

func TestHealthcheckCLICompatibilityAndStringForm(t *testing.T) {
	healthcheck, err := buildHealthcheck(&Healthcheck{Test: "wget -q localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(healthcheck.Test, []string{"CMD-SHELL", "wget -q localhost"}) {
		t.Fatalf("string healthcheck = %#v", healthcheck.Test)
	}
	healthcheck, err = buildHealthcheck(&Healthcheck{TestCLICompatible: true, Interval: "5s"})
	if err != nil {
		t.Fatal(err)
	}
	if healthcheck.Test != nil {
		t.Fatalf("CLI-compatible omitted test = %#v", healthcheck.Test)
	}
	healthcheck, err = buildHealthcheck(&Healthcheck{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(healthcheck.Test, []string{"NONE"}) {
		t.Fatalf("classic omitted test = %#v", healthcheck.Test)
	}
}

func TestMountTmpfsOptionsUseCanonicalListOfDictionaries(t *testing.T) {
	value := "1777"
	mount, err := buildMount(Mount{
		Type: "tmpfs", Target: "/cache",
		TmpfsOptions: []map[string]*string{{"nosuid": nil}, {"mode": &value}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mount.TmpfsOptions.Options, [][]string{{"nosuid"}, {"mode", "1777"}}) {
		t.Fatalf("tmpfs options = %#v", mount.TmpfsOptions.Options)
	}
	if _, err := buildMount(Mount{Type: "tmpfs", Target: "/cache", TmpfsOptions: []map[string]*string{{"a": nil, "b": nil}}}); err == nil {
		t.Fatal("multi-key tmpfs option was accepted")
	}
}

func TestPlatformNormalizationMatchesUpstreamSinglePartForms(t *testing.T) {
	tests := []struct {
		value, daemonOS, daemonArch string
		want                        string
	}{
		{"linux", "linux", "x86_64", "linux/amd64"},
		{"amd64", "linux", "amd64", "linux/amd64"},
		{"macos/arm64", "", "", "darwin/arm64"},
		{"linux/arm64/v8", "", "", "linux/arm64"},
		{"linux/arm/7", "", "", "linux/arm/v7"},
	}
	for _, test := range tests {
		platform, err := parsePlatform(test.value, test.daemonOS, test.daemonArch)
		if err != nil {
			t.Fatalf("parsePlatform(%q) error = %v", test.value, err)
		}
		actual := platform.OS + "/" + platform.Architecture
		if platform.Variant != "" {
			actual += "/" + platform.Variant
		}
		if actual != test.want {
			t.Fatalf("parsePlatform(%q) = %q, want %q", test.value, actual, test.want)
		}
	}
}

func TestImageIDIgnoresPullAlways(t *testing.T) {
	fake := &fakeContainerClient{}
	id := "sha256:" + strings.Repeat("a", 64)
	changed, action, response := ensureImage(context.Background(), fake, normalizeDefaults(Request{Name: "web", Image: id, Pull: PullAlways}), false, false)
	if changed || action != nil || response.Failed {
		t.Fatalf("image-ID pull result = changed %v, action %#v, response %#v", changed, action, response)
	}
}

func TestCheckModePullActionsMatchPinnedUpstream(t *testing.T) {
	missing := &fakeContainerClient{imageErr: errdefs.ErrNotFound.WithMessage("missing")}
	changed, action, response := ensureImage(context.Background(), missing, normalizeDefaults(Request{Name: "web", Image: "alpine:latest"}), false, true)
	if !changed || response.Failed || !reflect.DeepEqual(action, map[string]any{"pulled_image": "alpine:latest", "changed": true}) {
		t.Fatalf("missing-image check result = changed %v, action %#v, response %#v", changed, action, response)
	}
	present := &fakeContainerClient{}
	changed, action, response = ensureImage(context.Background(), present, normalizeDefaults(Request{
		Name: "web", Image: "alpine:latest", Pull: PullAlways, PullCheckModeBehavior: "always",
	}), true, true)
	if !changed || response.Failed || !reflect.DeepEqual(action, map[string]any{"pulled_image": "alpine:latest"}) {
		t.Fatalf("always-pull check result = changed %v, action %#v, response %#v", changed, action, response)
	}
}

func TestComparisonModesMatchUpstreamContainerSemantics(t *testing.T) {
	tests := []struct {
		name             string
		desired, current any
		mode, kind       string
		want             bool
	}{
		{"ordered-list-subsequence", []string{"one", "three"}, []string{"one", "two", "three"}, "allow_more_present", "list", true},
		{"ordered-list-wrong-order", []string{"three", "one"}, []string{"one", "two", "three"}, "allow_more_present", "list", false},
		{"strict-set-ignores-order", []string{"two", "one"}, []string{"one", "two"}, "strict", "set", true},
		{"strict-set-rejects-extra", []string{"one"}, []string{"one", "two"}, "strict", "set", false},
		{"dictionary-subset", map[string]string{"one": "1"}, map[string]string{"one": "1", "two": "2"}, "allow_more_present", "dict", true},
		{"strict-dictionary-rejects-extra", map[string]string{"one": "1"}, map[string]string{"one": "1", "two": "2"}, "strict", "dict", false},
		{"set-dictionary-subset", []map[string]any{{"path": "/dev/a"}}, []map[string]any{{"path": "/dev/a", "rate": 1}}, "allow_more_present", "set(dict)", true},
		{"strict-set-dictionary-rejects-extra", []map[string]any{{"path": "/dev/a"}}, []map[string]any{{"path": "/dev/a"}, {"path": "/dev/b"}}, "strict", "set(dict)", false},
		{"nil-desired-allows-current-set", nil, []string{"one"}, "allow_more_present", "set", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := compareValues(test.desired, test.current, test.mode, test.kind); actual != test.want {
				t.Fatalf("compareValues() = %v, want %v", actual, test.want)
			}
		})
	}
}

func TestStopTimeoutIsIgnoredUnlessComparisonIsOverridden(t *testing.T) {
	request := normalizeDefaults(Request{Name: "web", StopTimeout: intPointer(5)})
	if mode := comparisonMode(request, "stop_timeout"); mode != "ignore" {
		t.Fatalf("default stop_timeout comparison = %q", mode)
	}
	request.Comparisons = map[string]string{"stop_timeout": "strict"}
	if mode := comparisonMode(request, "stop_timeout"); mode != "strict" {
		t.Fatalf("overridden stop_timeout comparison = %q", mode)
	}
}

func TestImagePlatformNormalizesImplicitArm64V8(t *testing.T) {
	actual := imagePlatform(image.InspectResponse{Os: "linux", Architecture: "arm64", Variant: "v8"})
	if actual != "linux/arm64" {
		t.Fatalf("imagePlatform() = %q", actual)
	}
}

func TestVolumeNormalizationMatchesUpstream(t *testing.T) {
	values, err := normalizeVolumes([]string{"/tmp:/data", "/anon:rw", "/:/root:ro,z"}, docker.OSFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, []string{"/tmp:/data:rw", "/anon:rw", "/:/root:ro,z"}) {
		t.Fatalf("normalized volumes = %#v", values)
	}
	volumes, binds := splitVolumes(values)
	if _, found := volumes["/anon:rw"]; !found || !reflect.DeepEqual(binds, []string{"/tmp:/data:rw", "/:/root:ro,z"}) {
		t.Fatalf("split volumes = %#v / %#v", volumes, binds)
	}
	if _, err := normalizeVolumes([]string{"/tmp:/data:invalid"}, docker.OSFileSystem{}); err == nil {
		t.Fatal("invalid volume mode was accepted")
	}
}

func TestMountComparisonUsesOnlySuppliedSuboptions(t *testing.T) {
	desired := desiredMountComparison([]Mount{{Type: "bind", Source: "/tmp", Target: "/data"}})
	current := currentMountComparison([]mounttypes.Mount{{
		Type: mounttypes.TypeBind, Source: "/tmp", Target: "/data", ReadOnly: false,
		BindOptions: &mounttypes.BindOptions{Propagation: mounttypes.PropagationRPrivate},
	}})
	if !compareValues(desired, current, "allow_more_present", "set(dict)") {
		t.Fatalf("mounts did not compare as a supplied-suboption subset: %#v / %#v", desired, current)
	}
}

func TestNoDefaultsAttachmentMatchesPinnedUpstream(t *testing.T) {
	config, _, err := buildContainerConfig(normalizeDefaults(Request{Name: "web"}), recordingFileSystem{})
	if err != nil {
		t.Fatal(err)
	}
	if !config.AttachStdout || !config.AttachStderr || config.AttachStdin || config.OpenStdin {
		t.Fatalf("omitted detach/interactivity config = %#v", config)
	}
}

func TestCommandHandlingMatchesPinnedModes(t *testing.T) {
	correct, err := parseCommand([]string{"echo hello", "world"}, "correct")
	if err != nil || !reflect.DeepEqual(correct, []string{"echo hello", "world"}) {
		t.Fatalf("correct command = %#v, %v", correct, err)
	}
	compatibility, err := parseCommand([]string{"echo hello", "world"}, "compatibility")
	if err != nil || !reflect.DeepEqual(compatibility, []string{"echo", "hello", "world"}) {
		t.Fatalf("compatibility command = %#v, %v", compatibility, err)
	}
	entrypoint, err := parseEntrypoint([]string{"/bin/sh -c"}, "compatibility")
	if err != nil || !reflect.DeepEqual(entrypoint, []string{"/bin/sh", "-c"}) {
		t.Fatalf("compatibility entrypoint = %#v, %v", entrypoint, err)
	}
}

func TestVolumePathExpansionUsesInjectedFileSystem(t *testing.T) {
	values, err := normalizeVolumes([]string{"~:/home-data", "./relative:/work"}, recordingFileSystem{
		home: "/users/test", abs: map[string]string{"./relative": "/workspace/relative"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/users/test:/home-data:rw", "/workspace/relative:/work:rw"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("expanded volumes = %#v, want %#v", values, want)
	}
}

func TestNonPositiveHealthyWaitTimeoutDisablesTimeout(t *testing.T) {
	for _, value := range []float64{0, -1} {
		request := normalizeDefaults(Request{Name: "web", HealthyWaitTimeout: &value})
		if request.HealthyWaitTimeout != nil {
			t.Fatalf("healthy_wait_timeout=%g normalized to %#v, want no timeout", value, request.HealthyWaitTimeout)
		}
	}
}

func TestHealthyWaitRejectsUnexpectedEngineState(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	fake := &fakeContainerClient{inspect: container.InspectResponse{
		ID: "container-id", State: &container.State{Health: &container.Health{Status: "invalid"}},
	}}
	timeout := 30.0
	_, err := waitForHealthy(context.Background(), fake, "container-id", &timeout, clock)
	if err == nil || !strings.Contains(err.Error(), "unexpected health state") {
		t.Fatalf("waitForHealthy() error = %v", err)
	}
}

func TestRemovalAlreadyInProgressIsSuccessful(t *testing.T) {
	fake := &fakeContainerClient{removeErr: errors.New("removal of container abc is already in progress")}
	response := removeContainer(context.Background(), fake, normalizeDefaults(Request{Name: "web"}), container.InspectResponse{ID: "abc", State: &container.State{}})
	if response.Failed || !response.Changed {
		t.Fatalf("removeContainer() response = %#v", response)
	}
}

func TestForegroundNonReadableLoggingDriverReturnsSyntheticOutput(t *testing.T) {
	fake := &fakeContainerClient{inspect: container.InspectResponse{
		ID: "container-id", State: &container.State{}, Config: &container.Config{},
		HostConfig: &container.HostConfig{LogConfig: container.LogConfig{Type: "none"}},
	}}
	response := waitForExit(context.Background(), fake, Request{OutputLogs: true}, "container-id", true)
	if response.Failed || response.Stdout != "" || response.Container["Output"] != "Result logged using `none` driver" {
		t.Fatalf("waitForExit() response = %#v", response)
	}
	if fake.logsCalled != 0 {
		t.Fatalf("non-readable driver triggered %d logs calls", fake.logsCalled)
	}
}

func TestForegroundAutoRemovePreservesFailureStatus(t *testing.T) {
	fake := &fakeContainerClient{waitStatus: 7, inspect: container.InspectResponse{
		ID: "container-id", State: &container.State{}, Config: &container.Config{}, HostConfig: &container.HostConfig{},
	}}
	response := waitForExit(context.Background(), fake, Request{AutoRemove: boolPointer(true), OutputLogs: true}, "container-id", true)
	if !response.Failed || response.Status == nil || *response.Status != 7 || response.Stdout != "" || response.Msg != "Cannot retrieve result as auto_remove is enabled" {
		t.Fatalf("waitForExit() response = %#v", response)
	}
}

func TestConvertContainerReturnsFullDockerInspectionShape(t *testing.T) {
	converted := convertContainer(container.InspectResponse{
		ID: "container-id", Created: "2026-08-12T12:00:00Z", Path: "/bin/sh",
		Args: []string{"-c", "true"}, Driver: "overlay2", RestartCount: 2,
		Config: &container.Config{Image: "alpine:latest"}, HostConfig: &container.HostConfig{},
	})
	for key, expected := range map[string]any{
		"Id": "container-id", "Created": "2026-08-12T12:00:00Z", "Path": "/bin/sh",
		"Driver": "overlay2", "RestartCount": float64(2),
	} {
		if !reflect.DeepEqual(converted[key], expected) {
			t.Fatalf("inspection field %s = %#v, want %#v", key, converted[key], expected)
		}
	}
	if !reflect.DeepEqual(converted["Args"], []any{"-c", "true"}) {
		t.Fatalf("inspection args = %#v", converted["Args"])
	}
}

func intPointer(value int) *int          { return &value }
func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }
