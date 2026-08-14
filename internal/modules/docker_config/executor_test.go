package docker_config

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

type configClient struct {
	client.APIClient
	items     map[string]swarm.Config
	creates   []swarm.ConfigSpec
	removed   []string
	listErr   error
	createErr error
	removeErr error
	nextID    int
}

func (fake *configClient) Close() error { return nil }

func (fake *configClient) ConfigList(_ context.Context, options client.ConfigListOptions) (client.ConfigListResult, error) {
	if fake.listErr != nil {
		return client.ConfigListResult{}, fake.listErr
	}
	var items []swarm.Config
	for _, item := range fake.items {
		if configMatches(item, options.Filters) {
			items = append(items, item)
		}
	}
	return client.ConfigListResult{Items: items}, nil
}

func (fake *configClient) ConfigCreate(_ context.Context, options client.ConfigCreateOptions) (client.ConfigCreateResult, error) {
	fake.creates = append(fake.creates, options.Spec)
	if fake.createErr != nil {
		return client.ConfigCreateResult{}, fake.createErr
	}
	fake.nextID++
	id := fmt.Sprintf("cfg-%d", fake.nextID)
	for _, exists := fake.items[id]; exists; _, exists = fake.items[id] {
		fake.nextID++
		id = fmt.Sprintf("cfg-%d", fake.nextID)
	}
	fake.items[id] = swarm.Config{ID: id, Spec: options.Spec}
	return client.ConfigCreateResult{ID: id}, nil
}

func (fake *configClient) ConfigRemove(_ context.Context, id string, _ client.ConfigRemoveOptions) (client.ConfigRemoveResult, error) {
	fake.removed = append(fake.removed, id)
	if fake.removeErr != nil {
		return client.ConfigRemoveResult{}, fake.removeErr
	}
	delete(fake.items, id)
	for key, item := range fake.items {
		if item.Spec.Name == id {
			delete(fake.items, key)
			break
		}
	}
	return client.ConfigRemoveResult{}, nil
}

func configMatches(item swarm.Config, filters client.Filters) bool {
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

func configDependencies(fake *configClient) docker.Dependencies {
	if fake.items == nil {
		fake.items = map[string]swarm.Config{}
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

func execute(req Request, fake *configClient) Response {
	return ExecuteWithDependencies(req, configDependencies(fake))
}

func executeCheck(req Request, fake *configClient) Response {
	return ExecuteWithDependenciesAndState(req, configDependencies(fake), execution.State{CheckMode: true})
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
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
	invalidDriver := execute(Request{Name: "foo", Data: strPtr("x"), TemplateDriver: "not a template driver"}, fake)
	if !invalidDriver.Failed || invalidDriver.Msg != "value of template_driver must be one of: golang, got: not a template driver" {
		t.Fatalf("invalid driver = %#v", invalidDriver)
	}
	invalidState := execute(Request{Name: "foo", Data: strPtr("x"), State: "running"}, fake)
	if !invalidState.Failed || !strings.Contains(invalidState.Msg, "value of state must be one of: absent, present") {
		t.Fatalf("invalid state = %#v", invalidState)
	}
	if len(fake.creates) != 0 {
		t.Fatalf("validation must not talk to Engine: %#v", fake.creates)
	}
}

func TestConfigCreateIdempotentAndUpdate(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	created := execute(presentReq("db_password", "opensesame!"), fake)
	if created.Failed || !created.Changed || created.ConfigID == "" || created.ConfigName != "db_password" {
		t.Fatalf("create = %#v", created)
	}
	if len(fake.creates) != 1 || fake.creates[0].Labels[ansibleKeyLabel] != sha224Hex([]byte("opensesame!")) {
		t.Fatalf("create spec = %#v", fake.creates[0])
	}

	again := execute(presentReq("db_password", "opensesame!"), fake)
	if again.Failed || again.Changed || again.ConfigID != created.ConfigID {
		t.Fatalf("idempotent = %#v", again)
	}

	updated := execute(presentReq("db_password", "newpassword!"), fake)
	if updated.Failed || !updated.Changed || updated.ConfigID == created.ConfigID {
		t.Fatalf("update = %#v", updated)
	}
	if len(fake.removed) != 1 {
		t.Fatalf("non-rolling update should remove then create, removed=%#v", fake.removed)
	}
}

func TestConfigDataSrcAndBase64AreIdempotent(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	deps := configDependencies(fake)
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

func TestConfigDataSrcMissingFile(t *testing.T) {
	t.Parallel()
	response := ExecuteWithDependencies(Request{Name: "foo", DataSrc: "/missing"}, configDependencies(&configClient{}))
	if !response.Failed || !strings.Contains(response.Msg, "Error while reading /missing:") {
		t.Fatalf("response = %#v", response)
	}
}

func TestConfigEmptyDataIsValid(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	created := execute(presentReq("empty", ""), fake)
	if created.Failed || !created.Changed || len(fake.creates) != 1 || len(fake.creates[0].Data) != 0 {
		t.Fatalf("empty create = %#v spec=%#v", created, fake.creates)
	}
	again := execute(presentReq("empty", ""), fake)
	if again.Failed || again.Changed {
		t.Fatalf("empty idempotent = %#v", again)
	}
}

func TestConfigLabelsAllowMorePresentAndForce(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
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
	forced := execute(Request{Name: "foo", Data: strPtr("Goodnight everyone!"), Force: true, Labels: LabelMap{"bar": "baz", "one": "1", "two": "2"}}, fake)
	if forced.Failed || !forced.Changed || forced.ConfigID == added.ConfigID {
		t.Fatalf("force = %#v previous=%s", forced, added.ConfigID)
	}
}

func TestConfigMissingAnsibleKeyDoesNotChangeWithoutForce(t *testing.T) {
	t.Parallel()
	fake := &configClient{
		nextID: 7,
		items: map[string]swarm.Config{
			"cfg-1": {
				ID: "cfg-1",
				Spec: swarm.ConfigSpec{
					Annotations: swarm.Annotations{Name: "foo", Labels: map[string]string{"env": "prod"}},
					Data:        []byte("secret"),
				},
			},
		},
	}
	unchanged := execute(presentReq("foo", "other"), fake)
	if unchanged.Failed || unchanged.Changed || unchanged.ConfigID != "cfg-1" {
		t.Fatalf("missing ansible_key = %#v", unchanged)
	}
	forced := execute(Request{Name: "foo", Data: strPtr("other"), Force: true}, fake)
	if forced.Failed || !forced.Changed || forced.ConfigID == "cfg-1" {
		t.Fatalf("force missing key = %#v", forced)
	}
}

func TestConfigRollingVersionsAndPrune(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	first := execute(Request{Name: "rolling_password", Data: strPtr("opensesame!"), RollingVersions: true}, fake)
	if first.Failed || !first.Changed || first.ConfigName != "rolling_password_v1" {
		t.Fatalf("v1 = %#v", first)
	}
	if fake.creates[0].Labels[ansibleVersionLabel] != "1" {
		t.Fatalf("v1 labels = %#v", fake.creates[0].Labels)
	}

	second := execute(Request{Name: "rolling_password", Data: strPtr("newpassword!"), RollingVersions: true}, fake)
	if second.Failed || !second.Changed || second.ConfigName != "rolling_password_v2" || second.ConfigID == first.ConfigID {
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
	if keepOne.Failed || keepOne.ConfigName != "rolling_password_v3" {
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
	if keepZero.ConfigName != "rolling_password_v4" || len(fake.items) != 1 {
		t.Fatalf("versions_to_keep=0 = %#v items=%d", keepZero, len(fake.items))
	}

	keepAll := execute(Request{
		Name:            "rolling_password",
		Data:            strPtr("fifth!"),
		RollingVersions: true,
		VersionsToKeep:  intPtr(-1),
	}, fake)
	if keepAll.ConfigName != "rolling_password_v5" || len(fake.items) != 2 {
		t.Fatalf("versions_to_keep=-1 = %#v items=%d", keepAll, len(fake.items))
	}
}

func TestConfigRollingDefaultKeepsFive(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
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

func TestConfigTemplateDriver(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	created := execute(presentReq("db_password", "opensesame!"), fake)
	if created.Failed {
		t.Fatalf("create = %#v", created)
	}
	templated := execute(Request{Name: "db_password", Data: strPtr("opensesame!"), TemplateDriver: "golang"}, fake)
	if templated.Failed || !templated.Changed {
		t.Fatalf("add template_driver = %#v", templated)
	}
	if fake.creates[len(fake.creates)-1].Templating == nil || fake.creates[len(fake.creates)-1].Templating.Name != "golang" {
		t.Fatalf("templating spec = %#v", fake.creates[len(fake.creates)-1].Templating)
	}
	again := execute(Request{Name: "db_password", Data: strPtr("opensesame!"), TemplateDriver: "golang"}, fake)
	if again.Failed || again.Changed {
		t.Fatalf("golang idempotent = %#v", again)
	}
	templateData := execute(Request{Name: "db_password", Data: strPtr("{{ .Service.Name }}"), TemplateDriver: "golang"}, fake)
	if templateData.Failed || !templateData.Changed {
		t.Fatalf("template data = %#v", templateData)
	}
	last := fake.creates[len(fake.creates)-1]
	if string(last.Data) != "{{ .Service.Name }}" {
		t.Fatalf("template bytes = %q", last.Data)
	}
}

func TestConfigAbsentIdempotent(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
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

func TestConfigRollingAbsentRemovesAllVersions(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	execute(Request{Name: "rolling_password", Data: strPtr("a"), RollingVersions: true}, fake)
	execute(Request{Name: "rolling_password", Data: strPtr("b"), RollingVersions: true, VersionsToKeep: intPtr(-1)}, fake)
	removed := execute(Request{Name: "rolling_password", State: "absent", RollingVersions: true}, fake)
	if removed.Failed || !removed.Changed || len(fake.items) != 0 {
		t.Fatalf("rolling absent = %#v items=%d", removed, len(fake.items))
	}
}

func TestConfigCheckModeDoesNotMutate(t *testing.T) {
	t.Parallel()
	fake := &configClient{}
	predicted := executeCheck(presentReq("db_password", "opensesame!"), fake)
	if predicted.Failed || !predicted.Changed || predicted.ConfigID != "" || predicted.ConfigName != "db_password" {
		t.Fatalf("check create = %#v", predicted)
	}
	if len(fake.creates) != 0 || len(fake.items) != 0 {
		t.Fatalf("check create mutated Engine creates=%d items=%d", len(fake.creates), len(fake.items))
	}

	created := execute(presentReq("db_password", "opensesame!"), fake)
	checkSame := executeCheck(presentReq("db_password", "opensesame!"), fake)
	if checkSame.Failed || checkSame.Changed || checkSame.ConfigID != created.ConfigID {
		t.Fatalf("check idempotent = %#v", checkSame)
	}

	checkUpdate := executeCheck(presentReq("db_password", "newpassword!"), fake)
	if checkUpdate.Failed || !checkUpdate.Changed || checkUpdate.ConfigID != "" {
		t.Fatalf("check update = %#v", checkUpdate)
	}
	if len(fake.items) != 1 || len(fake.removed) != 0 {
		t.Fatalf("check update mutated Engine items=%d removed=%#v", len(fake.items), fake.removed)
	}

	checkRolling := executeCheck(Request{Name: "roll", Data: strPtr("a"), RollingVersions: true}, fake)
	if checkRolling.ConfigName != "roll_v1" || len(fake.items) != 1 {
		t.Fatalf("check rolling = %#v items=%d", checkRolling, len(fake.items))
	}
}

func TestConfigEngineErrors(t *testing.T) {
	t.Parallel()
	listFail := execute(presentReq("foo", "x"), &configClient{listErr: errors.New("boom")})
	if !listFail.Failed || listFail.Msg != "Error accessing config foo: boom" {
		t.Fatalf("list = %#v", listFail)
	}
	createFail := execute(presentReq("foo", "x"), &configClient{createErr: errors.New("nope")})
	if !createFail.Failed || createFail.Msg != "Error creating config: nope" {
		t.Fatalf("create = %#v", createFail)
	}
	removeFail := execute(Request{Name: "foo", State: "absent"}, &configClient{
		items: map[string]swarm.Config{
			"cfg-1": {ID: "cfg-1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "foo"}}},
		},
		removeErr: errors.New("busy"),
	})
	if !removeFail.Failed || removeFail.Msg != "Error removing config foo: busy" {
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

func TestConfigLabelMapSanitize(t *testing.T) {
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

func TestConfigVersionsToKeepZeroDecodes(t *testing.T) {
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
