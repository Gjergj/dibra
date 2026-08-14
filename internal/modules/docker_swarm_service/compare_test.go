package docker_swarm_service

import (
	"strings"
	"testing"
)

func TestRetryOnOutOfSequenceError(t *testing.T) {
	clock := &fakeClock{}
	fake := &serviceClient{updateErr: "rpc error: code = Unknown desc = update out of sequence"}
	fake.services = map[string]serviceRecord{
		"web": existingService("web", "alpine:latest", 1),
	}
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(2),
	}, serviceDependencies(fake, clock))
	if !response.Failed || !strings.Contains(response.Msg, "update out of sequence") {
		t.Fatalf("response = %#v", response)
	}
	if fake.updates != 3 {
		t.Fatalf("updates = %d want 3", fake.updates)
	}
	if clock.sleeps != 2 {
		t.Fatalf("sleeps = %d want 2", clock.sleeps)
	}
}

func TestNoRetryOnGeneralAPIError(t *testing.T) {
	clock := &fakeClock{}
	fake := &serviceClient{updateErr: "some error"}
	fake.services = map[string]serviceRecord{
		"web": existingService("web", "alpine:latest", 1),
	}
	response := ExecuteWithDependencies(Request{
		Name:     "web",
		Image:    "alpine:latest",
		Replicas: int64Ptr(2),
	}, serviceDependencies(fake, clock))
	if !response.Failed || !strings.Contains(response.Msg, "some error") {
		t.Fatalf("response = %#v", response)
	}
	if fake.updates != 1 || clock.sleeps != 0 {
		t.Fatalf("updates=%d sleeps=%d", fake.updates, clock.sleeps)
	}
}

func TestGetDockerEnvironment(t *testing.T) {
	readFile := func(string) ([]byte, error) {
		return []byte("TEST1=A\nTEST2=B\nTEST3=C\n"), nil
	}
	want := []string{"TEST1=A", "TEST2=B", "TEST3=CC", "TEST4=D"}

	fromDict, err := getDockerEnvironment(map[string]any{"TEST3": "CC", "TEST4": "D"}, []string{"dummypath"}, readFile)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, fromDict, want)

	fromList, err := getDockerEnvironment([]string{"TEST3=CC", "TEST4=D"}, []string{"dummypath"}, readFile)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, fromList, want)

	fromString, err := getDockerEnvironment("TEST3=CC,TEST4=D", []string{"dummypath"}, readFile)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, fromString, want)

	empty, err := getDockerEnvironment([]string{}, nil, readFile)
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("empty env = %#v err=%v", empty, err)
	}
	emptyFiles, err := getDockerEnvironment(nil, []string{}, readFile)
	if err != nil || len(emptyFiles) != 0 || emptyFiles == nil {
		t.Fatalf("empty files = %#v err=%v", emptyFiles, err)
	}
}

func TestGetNanosecondsFromRawOption(t *testing.T) {
	value, err := nanosecondsFromRaw("test", nil)
	if err != nil || value != nil {
		t.Fatalf("nil = %v %v", value, err)
	}
	value, err = nanosecondsFromRaw("test", "1m30s535ms")
	if err != nil || value == nil || *value != 90535000000 {
		t.Fatalf("duration = %v %v", pointerValue(value), err)
	}
	value, err = nanosecondsFromRaw("test", 10000000000)
	if err != nil || value == nil || *value != 10000000000 {
		t.Fatalf("int = %v %v", pointerValue(value), err)
	}
	if _, err = nanosecondsFromRaw("test", []string{}); err == nil {
		t.Fatal("expected type error")
	}
}

func TestHasDictChanged(t *testing.T) {
	assertFalse(t, hasDictChanged(map[string]any{"a": 1}, map[string]any{"a": 1}))
	assertFalse(t, hasDictChanged(map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}))
	assertTrue(t, hasDictChanged(map[string]any{"a": 1}, map[string]any{"a": 2, "b": 2}))
	assertTrue(t, hasDictChanged(map[string]any{"a": 1, "b": 1}, map[string]any{"a": 1}))
	assertFalse(t, hasDictChanged(nil, map[string]any{"a": 2, "b": 2}))
	assertTrue(t, hasDictChanged(map[string]any{}, map[string]any{"a": 2, "b": 2}))
	assertTrue(t, hasDictChanged(map[string]any{"a": 1}, map[string]any{}))
	assertTrue(t, hasDictChanged(map[string]any{"a": 1}, nil))
	assertFalse(t, hasDictChanged(map[string]any{}, map[string]any{}))
	assertFalse(t, hasDictChanged(nil, nil))
	assertFalse(t, hasDictChanged(map[string]any{}, nil))
	assertFalse(t, hasDictChanged(nil, map[string]any{}))
	assertFalse(t, hasDictChanged(map[string]any{"delay": int64(5000000000), "failure_action": "pause", "parallelism": uint64(1)}, map[string]any{"delay": int64(5000000000), "failure_action": failureAction("pause"), "parallelism": uint64(1), "monitor": int64(5000000000), "order": "stop-first"}))
	assertFalse(t, hasDictChanged(map[string]any{"tmpfs_mode": uint32(1023), "tmpfs_size": int64(67108864)}, map[string]any{"tmpfs_mode": uint32(1023), "tmpfs_size": int64(67108864)}))
}

type failureAction string

func TestHasPublishChangedHostMode(t *testing.T) {
	desired := []map[string]any{
		{"protocol": "udp", "published_port": uint32(60001), "target_port": uint32(60001), "mode": "host"},
		{"protocol": "tcp", "published_port": uint32(60002), "target_port": uint32(60003), "mode": "host"},
	}
	active := []map[string]any{
		{"protocol": "tcp", "published_port": uint32(60002), "target_port": uint32(60003), "mode": "host"},
		{"protocol": "udp", "published_port": uint32(60001), "target_port": uint32(60001), "mode": "host"},
	}
	assertFalse(t, hasPublishChanged(desired, active))
}

func TestHasListChanged(t *testing.T) {
	assertListChanged(t, nil, nil, true, "", false)
	assertListChanged(t, nil, []any{}, true, "", false)
	assertListChanged(t, nil, []any{1, 2}, true, "", false)
	assertListChanged(t, []any{}, nil, true, "", false)
	assertListChanged(t, []any{}, []any{}, true, "", false)
	assertListChanged(t, []any{}, []any{1, 2}, true, "", true)
	assertListChanged(t, []any{1, 2}, nil, true, "", true)
	assertListChanged(t, []any{1, 2}, []any{}, true, "", true)
	assertListChanged(t, []any{1, 2, 3}, []any{1, 2}, true, "", true)
	assertListChanged(t, []any{1, 2}, []any{1, 2, 3}, true, "", true)
	assertListChanged(t, []any{1, 2}, []any{2, 1}, true, "", false)
	assertListChanged(t, []any{1, 2}, []any{2, 1}, false, "", true)
	assertListChanged(t, []any{nil, 1}, []any{2, 1}, true, "", true)
	assertListChanged(t, []any{2, 1}, []any{nil, 1}, true, "", true)
	assertListChanged(t, []any{"command --with args"}, []any{"command", "--with", "args"}, true, "", true)
	assertListChanged(t, []any{"sleep", "3400"}, []any{"sleep", "3600"}, false, "", true)

	assertListChanged(t, []any{map[string]any{"a": 1}}, []any{map[string]any{"a": 1}}, true, "a", false)
	assertListChanged(t, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, true, "a", false)
	if _, err := hasListChanged([]any{map[string]any{"a": 1}, map[string]any{"a": 2}}, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, true, ""); err == nil {
		t.Fatal("expected missing sort key")
	}
	assertListChanged(t, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, []any{map[string]any{"a": 2}, map[string]any{"a": 1}}, true, "a", false)
	assertListChanged(t, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, []any{map[string]any{"a": 2}, map[string]any{"a": 1}}, false, "", true)
	assertListChanged(t, []any{map[string]any{"a": 1}, map[string]any{"a": 2}, map[string]any{"a": 3}}, []any{map[string]any{"a": 2}, map[string]any{"a": 1}}, true, "a", true)
	assertListChanged(t, []any{map[string]any{"a": 1}, map[string]any{"a": 2}}, []any{map[string]any{"a": 1}, map[string]any{"a": 2}, map[string]any{"a": 3}}, false, "", true)

	assertListChanged(t, []any{
		map[string]any{"src": 1, "dst": 2},
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
	}, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "tcp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
	}, true, "dst", false)
	assertListChanged(t, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 3, "protocol": "tcp"},
	}, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 3, "protocol": "tcp"},
	}, true, "dst", false)
	assertListChanged(t, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 2},
		map[string]any{"src": 3, "dst": 4},
	}, []any{
		map[string]any{"src": 1, "dst": 3, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "tcp"},
		map[string]any{"src": 3, "dst": 4, "protocol": "tcp"},
	}, true, "dst", true)
	assertListChanged(t, []any{
		map[string]any{"src": 1, "dst": 3, "protocol": "tcp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
	}, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "tcp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
	}, true, "dst", true)
	assertListChanged(t, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "tcp", "extra": map[string]any{"test": "foo"}},
	}, []any{
		map[string]any{"src": 1, "dst": 2, "protocol": "udp"},
		map[string]any{"src": 1, "dst": 2, "protocol": "tcp"},
	}, true, "dst", true)
	assertListChanged(t, []any{map[string]any{"id": "123", "aliases": []any{}}}, []any{map[string]any{"id": "123"}}, true, "id", false)
}

func TestHaveNetworksChanged(t *testing.T) {
	assertFalse(t, haveNetworksChanged(nil, nil))
	assertFalse(t, haveNetworksChanged([]map[string]any{}, nil))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}}, []map[string]any{{"id": 1}}))
	assertTrue(t, haveNetworksChanged([]map[string]any{{"id": 1}}, []map[string]any{{"id": 1}, {"id": 2}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2}}, []map[string]any{{"id": 1}, {"id": 2}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2}}, []map[string]any{{"id": 2}, {"id": 1}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2, "aliases": []any{}}}, []map[string]any{{"id": 1}, {"id": 2}}))
	assertTrue(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1"}}}, []map[string]any{{"id": 1}, {"id": 2}}))
	assertTrue(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1"}}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias2", "alias1"}}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1, "options": map[string]any{}}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1}, {"id": 2, "aliases": []any{"alias2", "alias1"}}}))
	assertFalse(t, haveNetworksChanged([]map[string]any{{"id": 1, "options": map[string]any{"option1": "value1"}}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1, "options": map[string]any{"option1": "value1"}}, {"id": 2, "aliases": []any{"alias2", "alias1"}}}))
	assertTrue(t, haveNetworksChanged([]map[string]any{{"id": 1, "options": map[string]any{"option1": "value1"}}, {"id": 2, "aliases": []any{"alias1", "alias2"}}}, []map[string]any{{"id": 1, "options": map[string]any{"option1": "value2"}}, {"id": 2, "aliases": []any{"alias2", "alias1"}}}))
}

func TestGetDockerNetworks(t *testing.T) {
	networkNames := []string{"network_1", "network_2", "network_3", "network_4"}
	networks := []any{
		networkNames[0],
		map[string]any{"name": networkNames[1]},
		map[string]any{"name": networkNames[2], "aliases": []any{"networkalias1"}},
		map[string]any{"name": networkNames[3], "aliases": []any{"networkalias2"}, "options": map[string]any{"foo": "bar"}},
	}
	networkIDs := map[string]string{
		networkNames[0]: "1",
		networkNames[1]: "2",
		networkNames[2]: "3",
		networkNames[3]: "4",
	}
	parsed, err := getDockerNetworks(networks, networkIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 4 {
		t.Fatalf("len = %d", len(parsed))
	}
	for i, network := range parsed {
		if _, found := network["name"]; found {
			t.Fatalf("name leaked: %#v", network)
		}
		if network["id"] != networkIDs[networkNames[i]] {
			t.Fatalf("id = %#v", network["id"])
		}
		if i == 2 {
			aliases, _ := network["aliases"].([]string)
			if len(aliases) != 1 || aliases[0] != "networkalias1" {
				t.Fatalf("aliases = %#v", network["aliases"])
			}
		}
		if i == 3 {
			options, _ := network["options"].(map[string]string)
			if options["foo"] != "bar" {
				t.Fatalf("options = %#v", network["options"])
			}
		}
	}
	if _, err := getDockerNetworks([]any{map[string]any{"invalid": "err"}}, map[string]string{"err": "x"}); err == nil || !strings.Contains(err.Error(), `"name" is required`) {
		t.Fatalf("missing name = %v", err)
	}
	if _, err := getDockerNetworks([]any{map[string]any{"name": "test", "aliases": 1}}, map[string]string{"test": "x"}); err == nil || !strings.Contains(err.Error(), `"aliases" network option is only allowed as a list`) {
		t.Fatalf("aliases type = %v", err)
	}
	if _, err := getDockerNetworks([]any{map[string]any{"name": "test", "aliases": []any{1}}}, map[string]string{"test": "x"}); err == nil || !strings.Contains(err.Error(), "Only strings are allowed as network aliases.") {
		t.Fatalf("aliases elements = %v", err)
	}
	if _, err := getDockerNetworks([]any{map[string]any{"name": "test", "options": 1}}, map[string]string{"test": "x"}); err == nil || !strings.Contains(err.Error(), "Only dict is allowed as network options.") {
		t.Fatalf("options type = %v", err)
	}
	if _, err := getDockerNetworks([]any{map[string]any{"name": "idontexist"}}, map[string]string{"test": "x"}); err == nil || !strings.Contains(err.Error(), "Could not find a network named: idontexist.") {
		t.Fatalf("missing network = %v", err)
	}
	empty, err := getDockerNetworks([]any{}, map[string]string{})
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("empty = %#v %v", empty, err)
	}
	none, err := getDockerNetworks(nil, map[string]string{})
	if err != nil || none != nil {
		t.Fatalf("none = %#v %v", none, err)
	}
	if _, err := getDockerNetworks([]any{map[string]any{"name": "test", "nonexisting_option": "foo"}}, map[string]string{"test": "1"}); err == nil || !strings.Contains(err.Error(), "nonexisting_option are not valid keys") {
		t.Fatalf("invalid option = %v", err)
	}
}

func TestParseCommandStringAndList(t *testing.T) {
	fromString, err := parseCommand(`/bin/sh -v -c "sleep 10m"`)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, fromString, []string{"/bin/sh", "-v", "-c", "sleep 10m"})
	fromList, err := parseCommand([]string{"/bin/sh", "-c", "sleep 10m"})
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, fromList, []string{"/bin/sh", "-c", "sleep 10m"})
}

func TestHasImageChangedStripsDigest(t *testing.T) {
	changed, active := hasImageChanged("alpine:latest", "alpine:latest@sha256:abc")
	if changed || active != "alpine:latest" {
		t.Fatalf("changed=%v active=%q", changed, active)
	}
	changed, _ = hasImageChanged("alpine:latest@sha256:abc", "alpine:latest@sha256:def")
	if !changed {
		t.Fatal("digest mismatch should change")
	}
}

func assertListChanged(t *testing.T, left, right []any, sortLists bool, sortKey string, want bool) {
	t.Helper()
	got, err := hasListChanged(left, right, sortLists, sortKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hasListChanged(%#v, %#v) = %v want %v", left, right, got, want)
	}
}

func assertTrue(t *testing.T, value bool) {
	t.Helper()
	if !value {
		t.Fatal("want true")
	}
}

func assertFalse(t *testing.T, value bool) {
	t.Helper()
	if value {
		t.Fatal("want false")
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
}

func int64Ptr(value int64) *int64 { return &value }
