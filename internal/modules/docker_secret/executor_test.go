package docker_secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

type secretClient struct {
	client.APIClient
	items     map[string]swarm.Secret
	creates   []swarm.SecretSpec
	removed   []string
	listErr   error
	createErr error
	removeErr error
	nextID    int
}

func (fake *secretClient) Close() error { return nil }

func (fake *secretClient) SecretList(_ context.Context, options client.SecretListOptions) (client.SecretListResult, error) {
	if fake.listErr != nil {
		return client.SecretListResult{}, fake.listErr
	}
	var items []swarm.Secret
	for _, item := range fake.items {
		if secretMatches(item, options.Filters) {
			items = append(items, item)
		}
	}
	return client.SecretListResult{Items: items}, nil
}

func (fake *secretClient) SecretCreate(_ context.Context, options client.SecretCreateOptions) (client.SecretCreateResult, error) {
	fake.creates = append(fake.creates, options.Spec)
	if fake.createErr != nil {
		return client.SecretCreateResult{}, fake.createErr
	}
	fake.nextID++
	id := fmt.Sprintf("sec-%d", fake.nextID)
	for _, exists := fake.items[id]; exists; _, exists = fake.items[id] {
		fake.nextID++
		id = fmt.Sprintf("sec-%d", fake.nextID)
	}
	fake.items[id] = swarm.Secret{ID: id, Spec: options.Spec}
	return client.SecretCreateResult{ID: id}, nil
}

func (fake *secretClient) SecretRemove(_ context.Context, id string, _ client.SecretRemoveOptions) (client.SecretRemoveResult, error) {
	fake.removed = append(fake.removed, id)
	if fake.removeErr != nil {
		return client.SecretRemoveResult{}, fake.removeErr
	}
	delete(fake.items, id)
	for key, item := range fake.items {
		if item.Spec.Name == id {
			delete(fake.items, key)
			break
		}
	}
	return client.SecretRemoveResult{}, nil
}

func secretMatches(item swarm.Secret, filters client.Filters) bool {
	if len(filters) == 0 {
		return true
	}
	if names := filters["name"]; len(names) > 0 {
		matched := false
		for name := range names {
			if strings.Contains(item.Spec.Name, name) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if ids := filters["id"]; len(ids) > 0 {
		if !ids[item.ID] {
			return false
		}
	}
	return true
}

type memoryFS struct {
	docker.FileSystem
	files map[string][]byte
}

func (fileSystem memoryFS) ReadFile(path string) ([]byte, error) {
	data, found := fileSystem.files[path]
	if !found {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func secretDependencies(fake *secretClient) docker.Dependencies {
	if fake.items == nil {
		fake.items = map[string]swarm.Secret{}
	}
	return docker.Dependencies{
		Environment: docker.StaticEnvironment{},
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return fake, nil
		},
		FileSystem: memoryFS{files: map[string][]byte{}},
	}
}

func presentReq(name, data string) Request {
	return Request{Name: name, Data: strPtr(data)}
}

func strPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func execute(req Request, fake *secretClient) Response {
	return ExecuteWithDependencies(req, secretDependencies(fake))
}

func executeCheck(req Request, fake *secretClient) Response {
	return ExecuteWithDependenciesAndState(req, secretDependencies(fake), execution.State{CheckMode: true})
}

func TestSecretValidation(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	missingName := execute(Request{Data: strPtr("x")}, fake)
	if !missingName.Failed || missingName.Msg != "missing required arguments: name" {
		t.Fatalf("missing name = %#v", missingName)
	}
	emptyName := execute(Request{Name: "  ", Data: strPtr("x")}, fake)
	if !emptyName.Failed || emptyName.Msg != "missing required arguments: name" {
		t.Fatalf("empty name = %#v", emptyName)
	}
	missingData := execute(Request{Name: "foo"}, fake)
	if !missingData.Failed || missingData.Msg != "state is present but any of the following are missing: data, data_src" {
		t.Fatalf("missing data = %#v", missingData)
	}
	both := execute(Request{Name: "foo", Data: strPtr("x"), DataSrc: "/tmp/data"}, fake)
	if !both.Failed || both.Msg != "parameters are mutually exclusive: data, data_src" {
		t.Fatalf("exclusive = %#v", both)
	}
	invalidState := execute(Request{Name: "foo", Data: strPtr("x"), State: "running"}, fake)
	if !invalidState.Failed || !strings.Contains(invalidState.Msg, "value of state must be one of: absent, present") {
		t.Fatalf("invalid state = %#v", invalidState)
	}
	if len(fake.creates) != 0 {
		t.Fatalf("validation must not talk to Engine: %#v", fake.creates)
	}
}

func TestSecretCreateIdempotentAndUpdate(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	created := execute(presentReq("db_password", "opensesame!"), fake)
	if created.Failed || !created.Changed || created.SecretID == "" || created.SecretName != "db_password" {
		t.Fatalf("create = %#v", created)
	}
	if len(fake.creates) != 1 || fake.creates[0].Labels[ansibleKeyLabel] != sha224Hex([]byte("opensesame!")) {
		t.Fatalf("create spec = %#v", fake.creates[0])
	}

	again := execute(presentReq("db_password", "opensesame!"), fake)
	if again.Failed || again.Changed || again.SecretID != created.SecretID {
		t.Fatalf("idempotent = %#v", again)
	}

	updated := execute(presentReq("db_password", "newpassword!"), fake)
	if updated.Failed || !updated.Changed || updated.SecretID == created.SecretID {
		t.Fatalf("update = %#v", updated)
	}
	if len(fake.removed) != 1 {
		t.Fatalf("non-rolling update should remove then create, removed=%#v", fake.removed)
	}
}

func TestSecretDataSrcAndBase64AreIdempotent(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	deps := secretDependencies(fake)
	deps.FileSystem = memoryFS{files: map[string][]byte{"/tmp/data": []byte("opensesame!")}}

	created := ExecuteWithDependencies(presentReq("db_password", "opensesame!"), deps)
	if created.Failed || !created.Changed {
		t.Fatalf("create = %#v", created)
	}
	fromFile := ExecuteWithDependencies(Request{Name: "db_password", DataSrc: "/tmp/data"}, deps)
	if fromFile.Failed || fromFile.Changed {
		t.Fatalf("data_src = %#v", fromFile)
	}
	fromB64 := ExecuteWithDependencies(Request{Name: "db_password", Data: strPtr("b3BlbnNlc2FtZSE="), DataIsB64: true}, deps)
	if fromB64.Failed || fromB64.Changed {
		t.Fatalf("data_is_b64 = %#v", fromB64)
	}
}

func TestSecretDataSrcCreatesFromFile(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	deps := secretDependencies(fake)
	deps.FileSystem = memoryFS{files: map[string][]byte{"/tmp/secret": []byte("file-secret")}}
	created := ExecuteWithDependencies(Request{Name: "from_file", DataSrc: "/tmp/secret"}, deps)
	if created.Failed || !created.Changed || created.SecretName != "from_file" {
		t.Fatalf("data_src create = %#v", created)
	}
	if len(fake.creates) != 1 || string(fake.creates[0].Data) != "file-secret" {
		t.Fatalf("data_src spec = %#v", fake.creates)
	}
	if fake.creates[0].Labels[ansibleKeyLabel] != sha224Hex([]byte("file-secret")) {
		t.Fatalf("data_src ansible_key = %#v", fake.creates[0].Labels)
	}
}

func TestSecretDataSrcMissingFile(t *testing.T) {
	t.Parallel()
	response := ExecuteWithDependencies(Request{Name: "foo", DataSrc: "/missing"}, secretDependencies(&secretClient{}))
	if !response.Failed || !strings.Contains(response.Msg, "Error while reading /missing:") {
		t.Fatalf("response = %#v", response)
	}
}

func TestSecretEmptyDataIsValid(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	created := execute(presentReq("empty", ""), fake)
	if created.Failed || !created.Changed || len(fake.creates) != 1 || len(fake.creates[0].Data) != 0 {
		t.Fatalf("empty create = %#v spec=%#v", created, fake.creates)
	}
	again := execute(presentReq("empty", ""), fake)
	if again.Failed || again.Changed {
		t.Fatalf("empty idempotent = %#v", again)
	}
}

func TestSecretBinaryBase64(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	payload := []byte{0x00, 0xff, 0xfe, 0x01}
	encoded := "AP/+AQ=="
	created := execute(Request{Name: "bin", Data: strPtr(encoded), DataIsB64: true}, fake)
	if created.Failed || !created.Changed || string(fake.creates[0].Data) != string(payload) {
		t.Fatalf("binary create = %#v spec=%#v", created, fake.creates)
	}
	again := execute(Request{Name: "bin", Data: strPtr(encoded), DataIsB64: true}, fake)
	if again.Failed || again.Changed {
		t.Fatalf("binary idempotent = %#v", again)
	}
}

func TestSecretInvalidBase64(t *testing.T) {
	t.Parallel()
	response := execute(Request{Name: "foo", Data: strPtr("!!!not-base64!!!"), DataIsB64: true}, &secretClient{})
	if !response.Failed || !strings.Contains(response.Msg, "Error while decoding base64 data:") {
		t.Fatalf("invalid b64 = %#v", response)
	}
}

func TestSecretLabelsAllowMorePresentAndForce(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	created := execute(Request{
		Name: "foo",
		Data: strPtr("Goodnight everyone!"),
		Labels: LabelMap{
			"bar": "baz",
			"one": "1",
		},
	}, fake)
	if created.Failed || !created.Changed {
		t.Fatalf("create = %#v", created)
	}
	less := execute(Request{
		Name: "foo",
		Data: strPtr("Goodnight everyone!"),
		Labels: LabelMap{
			"bar": "baz",
			"one": "1",
		},
	}, fake)
	if less.Failed || less.Changed {
		t.Fatalf("same labels = %#v", less)
	}
	added := execute(Request{
		Name: "foo",
		Data: strPtr("Goodnight everyone!"),
		Labels: LabelMap{
			"bar": "baz",
			"one": "1",
			"two": "2",
		},
	}, fake)
	if added.Failed || !added.Changed {
		t.Fatalf("added label = %#v", added)
	}
	dropped := execute(Request{
		Name: "foo",
		Data: strPtr("Goodnight everyone!"),
		Labels: LabelMap{
			"bar": "baz",
			"one": "1",
		},
	}, fake)
	if dropped.Failed || dropped.Changed {
		t.Fatalf("omitting extra labels should be unchanged = %#v", dropped)
	}
	changed := execute(Request{
		Name: "foo",
		Data: strPtr("Goodnight everyone!"),
		Labels: LabelMap{
			"bar": "monkey",
			"one": "1",
		},
	}, fake)
	if changed.Failed || !changed.Changed {
		t.Fatalf("label value change = %#v", changed)
	}
	forced := execute(Request{Name: "foo", Data: strPtr("Goodnight everyone!"), Force: true, Labels: LabelMap{"bar": "monkey", "one": "1"}}, fake)
	if forced.Failed || !forced.Changed || forced.SecretID == changed.SecretID {
		t.Fatalf("force = %#v previous=%s", forced, changed.SecretID)
	}
}

func TestSecretMissingAnsibleKeyDoesNotChangeWithoutForce(t *testing.T) {
	t.Parallel()
	fake := &secretClient{
		nextID: 7,
		items: map[string]swarm.Secret{
			"sec-1": {
				ID: "sec-1",
				Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{Name: "foo", Labels: map[string]string{"env": "prod"}},
					Data:        []byte("secret"),
				},
			},
		},
	}
	unchanged := execute(presentReq("foo", "other"), fake)
	if unchanged.Failed || unchanged.Changed || unchanged.SecretID != "sec-1" {
		t.Fatalf("missing ansible_key = %#v", unchanged)
	}
	forced := execute(Request{Name: "foo", Data: strPtr("other"), Force: true}, fake)
	if forced.Failed || !forced.Changed || forced.SecretID == "sec-1" {
		t.Fatalf("force missing key = %#v", forced)
	}
}

func TestSecretIgnoresSubstringNameMatches(t *testing.T) {
	t.Parallel()
	fake := &secretClient{
		nextID: 3,
		items: map[string]swarm.Secret{
			"sec-other": {
				ID: "sec-other",
				Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{Name: "db_password_extra", Labels: map[string]string{ansibleKeyLabel: "x"}},
				},
			},
		},
	}
	created := execute(presentReq("db_password", "opensesame!"), fake)
	if created.Failed || !created.Changed || created.SecretName != "db_password" {
		t.Fatalf("exact name create = %#v", created)
	}
}

func TestSecretRollingVersionsAndPrune(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	first := execute(Request{Name: "rolling_password", Data: strPtr("opensesame!"), RollingVersions: true}, fake)
	if first.Failed || !first.Changed || first.SecretName != "rolling_password_v1" {
		t.Fatalf("v1 = %#v", first)
	}
	if fake.creates[0].Labels[ansibleVersionLabel] != "1" {
		t.Fatalf("v1 labels = %#v", fake.creates[0].Labels)
	}

	second := execute(Request{Name: "rolling_password", Data: strPtr("newpassword!"), RollingVersions: true}, fake)
	if second.Failed || !second.Changed || second.SecretName != "rolling_password_v2" || second.SecretID == first.SecretID {
		t.Fatalf("v2 = %#v", second)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("rolling update must not delete first: %#v", fake.removed)
	}

	keepOne := execute(Request{
		Name:            "rolling_password",
		Data:            strPtr("third!"),
		RollingVersions: true,
		VersionsToKeep:  intPtr(1),
	}, fake)
	if keepOne.Failed || keepOne.SecretName != "rolling_password_v3" {
		t.Fatalf("v3 = %#v", keepOne)
	}
	if len(fake.items) != 1 {
		t.Fatalf("versions_to_keep=1 should keep only current, items=%d removed=%#v", len(fake.items), fake.removed)
	}

	keepZero := execute(Request{
		Name:            "rolling_password",
		Data:            strPtr("fourth!"),
		RollingVersions: true,
		VersionsToKeep:  intPtr(0),
	}, fake)
	if keepZero.SecretName != "rolling_password_v4" || len(fake.items) != 1 {
		t.Fatalf("versions_to_keep=0 = %#v items=%d", keepZero, len(fake.items))
	}

	keepAll := execute(Request{
		Name:            "rolling_password",
		Data:            strPtr("fifth!"),
		RollingVersions: true,
		VersionsToKeep:  intPtr(-1),
	}, fake)
	if keepAll.SecretName != "rolling_password_v5" || len(fake.items) != 2 {
		t.Fatalf("versions_to_keep=-1 = %#v items=%d", keepAll, len(fake.items))
	}
}

func TestSecretRollingDefaultKeepsFive(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	for i := 1; i <= 6; i++ {
		response := execute(Request{
			Name:            "keep",
			Data:            strPtr(fmt.Sprintf("data-%d", i)),
			RollingVersions: true,
		}, fake)
		if response.Failed {
			t.Fatalf("create %d = %#v", i, response)
		}
	}
	if len(fake.items) != 5 {
		t.Fatalf("default versions_to_keep=5, got %d items", len(fake.items))
	}
}

func TestSecretRollingIdempotentDoesNotPruneUnnecessarily(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	first := execute(Request{Name: "roll", Data: strPtr("a"), RollingVersions: true}, fake)
	again := execute(Request{Name: "roll", Data: strPtr("a"), RollingVersions: true}, fake)
	if again.Failed || again.Changed || again.SecretID != first.SecretID || again.SecretName != "roll_v1" {
		t.Fatalf("rolling idempotent = %#v", again)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("idempotent rolling must not prune: %#v", fake.removed)
	}
}

func TestSecretAbsentIdempotent(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	missing := execute(Request{Name: "db_password", State: "absent"}, fake)
	if missing.Failed || missing.Changed {
		t.Fatalf("absent missing = %#v", missing)
	}
	created := execute(presentReq("db_password", "opensesame!"), fake)
	if created.Failed {
		t.Fatalf("create = %#v", created)
	}
	removed := execute(Request{Name: "db_password", State: "absent"}, fake)
	if removed.Failed || !removed.Changed || len(fake.items) != 0 {
		t.Fatalf("absent = %#v items=%d", removed, len(fake.items))
	}
	again := execute(Request{Name: "db_password", State: "absent"}, fake)
	if again.Failed || again.Changed {
		t.Fatalf("absent idempotent = %#v", again)
	}
}

func TestSecretRollingAbsentRemovesAllVersions(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	execute(Request{Name: "rolling_password", Data: strPtr("a"), RollingVersions: true}, fake)
	execute(Request{Name: "rolling_password", Data: strPtr("b"), RollingVersions: true, VersionsToKeep: intPtr(-1)}, fake)
	removed := execute(Request{Name: "rolling_password", State: "absent", RollingVersions: true}, fake)
	if removed.Failed || !removed.Changed || len(fake.items) != 0 {
		t.Fatalf("rolling absent = %#v items=%d", removed, len(fake.items))
	}
}

func TestSecretCheckModeDoesNotMutate(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	predicted := executeCheck(presentReq("db_password", "opensesame!"), fake)
	if predicted.Failed || !predicted.Changed || predicted.SecretID != "" || predicted.SecretName != "db_password" {
		t.Fatalf("check create = %#v", predicted)
	}
	if len(fake.creates) != 0 || len(fake.items) != 0 {
		t.Fatalf("check create mutated Engine creates=%d items=%d", len(fake.creates), len(fake.items))
	}

	created := execute(presentReq("db_password", "opensesame!"), fake)
	checkSame := executeCheck(presentReq("db_password", "opensesame!"), fake)
	if checkSame.Failed || checkSame.Changed || checkSame.SecretID != created.SecretID {
		t.Fatalf("check idempotent = %#v", checkSame)
	}

	checkUpdate := executeCheck(presentReq("db_password", "newpassword!"), fake)
	if checkUpdate.Failed || !checkUpdate.Changed || checkUpdate.SecretID != "" {
		t.Fatalf("check update = %#v", checkUpdate)
	}
	if len(fake.items) != 1 || len(fake.removed) != 0 {
		t.Fatalf("check update mutated Engine items=%d removed=%#v", len(fake.items), fake.removed)
	}

	checkRolling := executeCheck(Request{Name: "roll", Data: strPtr("a"), RollingVersions: true}, fake)
	if checkRolling.SecretName != "roll_v1" || len(fake.items) != 1 {
		t.Fatalf("check rolling = %#v items=%d", checkRolling, len(fake.items))
	}

	checkAbsent := executeCheck(Request{Name: "db_password", State: "absent"}, fake)
	if checkAbsent.Failed || !checkAbsent.Changed || len(fake.items) != 1 {
		t.Fatalf("check absent = %#v items=%d", checkAbsent, len(fake.items))
	}
}

func TestSecretCheckModeSkipsPrune(t *testing.T) {
	t.Parallel()
	fake := &secretClient{}
	execute(Request{Name: "keep", Data: strPtr("a"), RollingVersions: true, VersionsToKeep: intPtr(-1)}, fake)
	execute(Request{Name: "keep", Data: strPtr("b"), RollingVersions: true, VersionsToKeep: intPtr(-1)}, fake)
	before := len(fake.items)
	predicted := executeCheck(Request{
		Name:            "keep",
		Data:            strPtr("c"),
		RollingVersions: true,
		VersionsToKeep:  intPtr(1),
	}, fake)
	if predicted.Failed || !predicted.Changed || predicted.SecretName != "keep_v3" {
		t.Fatalf("check prune = %#v", predicted)
	}
	if len(fake.items) != before || len(fake.removed) != 0 {
		t.Fatalf("check mode must not prune items=%d removed=%#v", len(fake.items), fake.removed)
	}
}

func TestSecretEngineErrors(t *testing.T) {
	t.Parallel()
	listFail := execute(presentReq("foo", "x"), &secretClient{listErr: errors.New("boom")})
	if !listFail.Failed || listFail.Msg != "Error accessing secret foo: boom" {
		t.Fatalf("list = %#v", listFail)
	}
	createFail := execute(presentReq("foo", "x"), &secretClient{createErr: errors.New("nope")})
	if !createFail.Failed || createFail.Msg != "Error creating secret: nope" {
		t.Fatalf("create = %#v", createFail)
	}
	removeFail := execute(Request{Name: "foo", State: "absent"}, &secretClient{
		items: map[string]swarm.Secret{
			"sec-1": {ID: "sec-1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "foo"}}},
		},
		removeErr: errors.New("busy"),
	})
	if !removeFail.Failed || removeFail.Msg != "Error removing secret foo: busy" {
		t.Fatalf("remove = %#v", removeFail)
	}
	clientFail := ExecuteWithDependencies(presentReq("foo", "x"), docker.Dependencies{
		NewClient: func(docker.CommonArgs) (client.APIClient, error) {
			return nil, errors.New("dial")
		},
	})
	if !clientFail.Failed || clientFail.Msg != "An unexpected Docker error occurred: dial" {
		t.Fatalf("client = %#v", clientFail)
	}
}

func TestSecretLabelMapSanitize(t *testing.T) {
	t.Parallel()
	var labels LabelMap
	if err := json.Unmarshal([]byte(`{"one": 1, "ok": "x"}`), &labels); err != nil || labels["one"] != "1" || labels["ok"] != "x" {
		t.Fatalf("ints = %#v err=%v", labels, err)
	}
	if err := json.Unmarshal([]byte(`{"bad": true}`), &labels); err == nil || !strings.Contains(err.Error(), `The value true for "bad" of labels`) {
		t.Fatalf("bool err = %v", err)
	}
	if err := json.Unmarshal([]byte(`{"bad": 1.5}`), &labels); err == nil || !strings.Contains(err.Error(), `The value 1.5 for "bad" of labels`) {
		t.Fatalf("float err = %v", err)
	}
}

func TestSecretVersionsToKeepZeroDecodes(t *testing.T) {
	t.Parallel()
	var req Request
	if err := json.Unmarshal([]byte(`{"name":"foo","data":"x","rolling_versions":true,"versions_to_keep":0}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.VersionsToKeep == nil || *req.VersionsToKeep != 0 {
		t.Fatalf("versions_to_keep = %#v", req.VersionsToKeep)
	}
	if req.Data == nil || *req.Data != "x" {
		t.Fatalf("data = %#v", req.Data)
	}
}

func TestSecretInvalidVersionLabelSortsAsZero(t *testing.T) {
	t.Parallel()
	fake := &secretClient{
		nextID: 2,
		items: map[string]swarm.Secret{
			"sec-old": {
				ID: "sec-old",
				Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{
						Name: "roll_v1",
						Labels: map[string]string{
							ansibleKeyLabel:     sha224Hex([]byte("old")),
							ansibleVersionLabel: "not-a-number",
						},
					},
				},
			},
		},
	}
	updated := execute(Request{Name: "roll", Data: strPtr("new"), RollingVersions: true, VersionsToKeep: intPtr(-1)}, fake)
	if updated.Failed || updated.SecretName != "roll_v1" {
		t.Fatalf("invalid version should start at 0 then increment to 1 = %#v", updated)
	}
}
