package docker_container_copy_into

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	apicontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type copyClient struct {
	client.APIClient
	stats       map[string]apicontainer.PathStat
	archives    map[string][]byte
	header      *tar.Header
	data        []byte
	puts        int
	statErr     error
	copyFromErr error
	copyToErr   error
}

func newCopyClient() *copyClient {
	return &copyClient{
		stats:    map[string]apicontainer.PathStat{},
		archives: map[string][]byte{},
	}
}

func (fake *copyClient) ContainerStatPath(_ context.Context, _ string, options client.ContainerStatPathOptions) (client.ContainerStatPathResult, error) {
	if fake.statErr != nil {
		return client.ContainerStatPathResult{}, fake.statErr
	}
	stat, ok := fake.stats[options.Path]
	if !ok {
		return client.ContainerStatPathResult{}, errdefs.ErrNotFound.WithMessage("missing")
	}
	return client.ContainerStatPathResult{Stat: stat}, nil
}

func (fake *copyClient) CopyFromContainer(_ context.Context, _ string, options client.CopyFromContainerOptions) (client.CopyFromContainerResult, error) {
	if fake.copyFromErr != nil {
		return client.CopyFromContainerResult{}, fake.copyFromErr
	}
	archive, ok := fake.archives[options.SourcePath]
	if !ok {
		return client.CopyFromContainerResult{}, errdefs.ErrNotFound.WithMessage("missing")
	}
	return client.CopyFromContainerResult{Content: io.NopCloser(bytes.NewReader(archive))}, nil
}

func (fake *copyClient) CopyToContainer(_ context.Context, _ string, options client.CopyToContainerOptions) (client.CopyToContainerResult, error) {
	if fake.copyToErr != nil {
		return client.CopyToContainerResult{}, fake.copyToErr
	}
	archive, err := io.ReadAll(options.Content)
	if err != nil {
		return client.CopyToContainerResult{}, err
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	header, err := reader.Next()
	if err != nil {
		return client.CopyToContainerResult{}, err
	}
	headerCopy := *header
	fake.header = &headerCopy
	fake.data, err = io.ReadAll(reader)
	if err != nil {
		return client.CopyToContainerResult{}, err
	}
	fake.puts++
	path := filepath.ToSlash(filepath.Join(options.DestinationPath, header.Name))
	mode := os.FileMode(header.Mode & 0o777)
	if header.Mode&0o4000 != 0 {
		mode |= os.ModeSetuid
	}
	if header.Mode&0o2000 != 0 {
		mode |= os.ModeSetgid
	}
	if header.Mode&0o1000 != 0 {
		mode |= os.ModeSticky
	}
	size := header.Size
	linkTarget := ""
	if header.Typeflag == tar.TypeSymlink {
		mode |= os.ModeSymlink
		linkTarget = header.Linkname
		size = int64(len(linkTarget))
	}
	fake.stats[path] = apicontainer.PathStat{
		Name:       filepath.Base(path),
		Size:       size,
		Mode:       mode,
		LinkTarget: linkTarget,
	}
	fake.archives[path] = archive
	return client.CopyToContainerResult{}, nil
}

func (*copyClient) Close() error { return nil }

type fixedClock struct {
	docker.Clock
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

func TestModeParsersMatchUpstreamVectors(t *testing.T) {
	valid := map[string]string{
		"0777": "511", "777": "511", "0o777": "511",
		"0755": "493", "755": "493", "0o755": "493",
		"0644": "420", "644": "420", "0o644": "420",
		" 0644 ": "420", " 644 ": "420", " 0o644 ": "420",
		"-1": "-1",
	}
	for value, expected := range valid {
		for name, parser := range map[string]func(any) (*big.Int, error){
			"modern": parseModern,
			"octal":  parseOctalStringOnly,
		} {
			got, err := parser(value)
			if err != nil || got.String() != expected {
				t.Errorf("%s(%q) = %v, %v; want %s", name, value, got, err, expected)
			}
		}
	}

	huge, _ := new(big.Int).SetString("123456789012345678901234567890123456789012345678901234567890", 10)
	for _, value := range []any{0o777, 0o755, 0o644, 12345, huge} {
		got, err := parseModern(value)
		if err != nil || got.Cmp(integerValue(value)) != 0 {
			t.Errorf("parseModern(%v) = %v, %v", value, got, err)
		}
		if _, err := parseOctalStringOnly(value); err == nil || !strings.Contains(err.Error(), "must be an octal string") {
			t.Errorf("parseOctalStringOnly(%v) error = %v", value, err)
		}
	}

	for _, value := range []any{1.0, 755.5, []any{}, map[string]any{}} {
		if _, err := parseModern(value); err == nil || !strings.Contains(err.Error(), "octal string or an integer") {
			t.Errorf("parseModern(%v) error = %v", value, err)
		}
		if _, err := parseOctalStringOnly(value); err == nil || !strings.Contains(err.Error(), "octal string") {
			t.Errorf("parseOctalStringOnly(%v) error = %v", value, err)
		}
	}
	for _, value := range []string{"foo", "8", "9"} {
		if _, err := parseModern(value); err == nil {
			t.Errorf("parseModern(%q) succeeded", value)
		}
		if _, err := parseOctalStringOnly(value); err == nil {
			t.Errorf("parseOctalStringOnly(%q) succeeded", value)
		}
	}
}

func TestValidationAndModeSemantics(t *testing.T) {
	content := ""
	path := "/tmp/source"
	owner := 0
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "container", request: Request{Content: &content, ContainerPath: "/x", Mode: raw(`"0644"`)}, want: "container is required"},
		{name: "container path", request: Request{Container: "web", Content: &content, Mode: raw(`"0644"`)}, want: "container_path is required"},
		{name: "neither source", request: Request{Container: "web", ContainerPath: "/x"}, want: "exactly one"},
		{name: "both sources", request: Request{Container: "web", ContainerPath: "/x", Path: &path, Content: &content}, want: "exactly one"},
		{name: "content mode", request: Request{Container: "web", ContainerPath: "/x", Content: &content}, want: "required by 'content'"},
		{name: "owner group", request: Request{Container: "web", ContainerPath: "/x", Content: &content, Mode: raw(`420`), OwnerID: &owner}, want: "supplied together"},
		{name: "mode strategy", request: Request{Container: "web", ContainerPath: "/x", Content: &content, Mode: raw(`420`), ModeParse: "future"}, want: "must be one of"},
		{name: "negative", request: Request{Container: "web", ContainerPath: "/x", Content: &content, Mode: raw(`"-1"`), ModeParse: "modern"}, want: "must not be negative"},
		{name: "octal integer", request: Request{Container: "web", ContainerPath: "/x", Content: &content, Mode: raw(`420`), ModeParse: "octal_string_only"}, want: "must be an octal string"},
		{name: "bad base64", request: Request{Container: "web", ContainerPath: "/x", Content: stringPointer("%%%"), ContentIsB64: true, Mode: raw(`420`)}, want: "Cannot Base64 decode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := validateAndPrepareSource(test.request, docker.OSFileSystem{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}

	legacy, _, err := parseMode(raw(`"420"`), "legacy")
	if err != nil || legacy != 0o644 {
		t.Fatalf("legacy string mode = %#o, %v", legacy, err)
	}
	modern, _, err := parseMode(raw(`"0644"`), "modern")
	if err != nil || modern != 0o644 {
		t.Fatalf("modern string mode = %#o, %v", modern, err)
	}
}

func TestContentCopyCheckDiffIdempotencyAndForce(t *testing.T) {
	fake := newCopyClient()
	now := time.Date(2026, time.August, 11, 12, 30, 0, 0, time.UTC)
	ownerID, groupID := 1000, 1001
	content := "hello"
	request := Request{
		Container:     "web",
		Content:       &content,
		ContainerPath: "/tmp/message.txt",
		OwnerID:       &ownerID,
		GroupID:       &groupID,
		Mode:          []byte(`"0644"`),
		ModeParse:     "modern",
	}
	dependencies := docker.Dependencies{
		NewClient: func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
		Clock:     fixedClock{now: now},
	}

	check := ExecuteWithDependenciesAndState(request, dependencies, executionState(true, true))
	if check.Failed || !check.Changed || fake.puts != 0 {
		t.Fatalf("check response = %#v, puts=%d", check, fake.puts)
	}
	if check.Diff["before"] != "" || check.Diff["after"] != "hello" {
		t.Fatalf("check diff = %#v", check.Diff)
	}

	response := ExecuteWithDependencies(request, dependencies)
	if response.Failed || !response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if fake.header == nil || fake.header.Name != "message.txt" || !fake.header.ModTime.Equal(now) {
		t.Fatalf("tar header = %#v", fake.header)
	}
	if string(fake.data) != "hello" || fake.header.Uid != ownerID || fake.header.Gid != groupID {
		t.Errorf("tar payload = %q, uid=%d gid=%d", fake.data, fake.header.Uid, fake.header.Gid)
	}

	unchanged := ExecuteWithDependenciesAndState(request, dependencies, executionState(false, true))
	if unchanged.Failed || unchanged.Changed || fake.puts != 1 {
		t.Fatalf("unchanged response = %#v, puts=%d", unchanged, fake.puts)
	}
	if unchanged.Diff["before"] != "hello" || unchanged.Diff["after"] != "hello" {
		t.Fatalf("unchanged diff = %#v", unchanged.Diff)
	}

	force := true
	request.Force = &force
	forced := ExecuteWithDependencies(request, dependencies)
	if forced.Failed || !forced.Changed || fake.puts != 2 {
		t.Fatalf("forced response = %#v, puts=%d", forced, fake.puts)
	}

	other := "other"
	force = false
	request.Content = &other
	request.Force = &force
	skipped := ExecuteWithDependenciesAndState(request, dependencies, executionState(false, true))
	if skipped.Failed || skipped.Changed || fake.puts != 2 {
		t.Fatalf("force=false response = %#v, puts=%d", skipped, fake.puts)
	}
	if skipped.Diff["before"] != "hello" || skipped.Diff["after"] != "hello" ||
		skipped.Diff["after_header"] != "/tmp/message.txt" {
		t.Fatalf("force=false diff = %#v", skipped.Diff)
	}
}

func TestContainerAndLocalSymlinkBehavior(t *testing.T) {
	fake := newCopyClient()
	owner, group := 0, 0
	content := "target"
	fake.addRegular("/real", []byte(content), 0o644, owner, group)
	fake.addSymlink("/link", "real", 0o777, owner, group)
	request := contentRequest(content, "/link", owner, group)
	request.Follow = true
	dependencies := fakeDependencies(fake)

	response := ExecuteWithDependenciesAndState(request, dependencies, executionState(false, true))
	if response.Failed || response.Changed || response.ContainerPath != "/real" {
		t.Fatalf("follow response = %#v", response)
	}

	request.Follow = false
	response = ExecuteWithDependenciesAndState(request, dependencies, executionState(true, true))
	if response.Failed || !response.Changed || response.ContainerPath != "/link" || response.Diff["before"] != "real" {
		t.Fatalf("no-follow response = %#v", response)
	}

	fake.addSymlink("/a", "b", 0o777, owner, group)
	fake.addSymlink("/b", "a", 0o777, owner, group)
	request.ContainerPath = "/a"
	request.Follow = true
	response = ExecuteWithDependencies(request, dependencies)
	if !response.Failed || !strings.Contains(response.Msg, "infinite symbolic link loop") {
		t.Fatalf("loop response = %#v", response)
	}

	temp := t.TempDir()
	target := filepath.Join(temp, "target")
	link := filepath.Join(temp, "link")
	if err := os.WriteFile(target, []byte("local"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", link); err != nil {
		t.Fatal(err)
	}

	pathRequest := Request{
		Container:     "web",
		Path:          &link,
		ContainerPath: "/copied-link",
		LocalFollow:   boolPointer(false),
		OwnerID:       &owner,
		GroupID:       &group,
		Force:         boolPointer(true),
	}
	response = ExecuteWithDependencies(pathRequest, dependencies)
	if response.Failed || fake.header == nil || fake.header.Typeflag != tar.TypeSymlink || fake.header.Linkname != "target" {
		t.Fatalf("local symlink response = %#v, header=%#v", response, fake.header)
	}

	fake.addSymlink("/matching-link", "target", 0o600, 123, 456)
	pathRequest.ContainerPath = "/matching-link"
	pathRequest.Force = nil
	puts := fake.puts
	response = ExecuteWithDependenciesAndState(pathRequest, dependencies, executionState(false, true))
	if response.Failed || response.Changed || fake.puts != puts {
		t.Fatalf("matching symlink response = %#v, puts=%d", response, fake.puts)
	}
	if response.Diff["before"] != "target" || response.Diff["after"] != "target" {
		t.Fatalf("matching symlink diff = %#v", response.Diff)
	}

	pathRequest.ContainerPath = "/copied-target"
	pathRequest.LocalFollow = boolPointer(true)
	pathRequest.Force = boolPointer(true)
	response = ExecuteWithDependencies(pathRequest, dependencies)
	if response.Failed || fake.header.Typeflag != tar.TypeReg || string(fake.data) != "local" || fake.header.Mode != 0o640 {
		t.Fatalf("local follow response = %#v, header=%#v data=%q", response, fake.header, fake.data)
	}
}

func TestBinaryLargeDiffAndArchiveErrors(t *testing.T) {
	owner, group := 0, 0
	binary := string([]byte{'a', 0, 'b'})
	fake := newCopyClient()
	fake.addRegular("/binary", []byte(binary), 0o600, owner, group)
	request := contentRequest(binary, "/binary", owner, group)
	request.Mode = raw(`"0600"`)
	max := 1024
	request.MaxFileSizeForDiff = &max
	response := ExecuteWithDependenciesAndState(request, fakeDependencies(fake), executionState(false, true))
	if response.Failed || response.Changed || response.Diff["src_binary"] != 1 || response.Diff["dst_binary"] != 1 {
		t.Fatalf("binary response = %#v", response)
	}
	if _, ok := response.Diff["before"]; !ok {
		t.Fatalf("canonical binary diff = %#v", response.Diff)
	}

	temporaryDiff := map[string]any{}
	if err := addDestinationDiff(context.Background(), fake, "web", containerFile{
		path: "/temporary", found: true, mode: os.ModeTemporary,
	}, 1024, temporaryDiff, nil); err != nil {
		t.Fatal(err)
	}
	if temporaryDiff["before"] != "(temporary file)" {
		t.Fatalf("temporary diff = %#v", temporaryDiff)
	}

	large := strings.Repeat("x", 32)
	request = contentRequest(large, "/large", owner, group)
	max = 8
	request.MaxFileSizeForDiff = &max
	response = ExecuteWithDependenciesAndState(request, fakeDependencies(newCopyClient()), executionState(true, true))
	if response.Failed || response.Diff["src_larger"] != 8 || response.Diff["before"] != "" {
		t.Fatalf("large response = %#v", response)
	}

	fake = newCopyClient()
	fake.addRegular("/broken", []byte("value"), 0o644, owner, group)
	fake.archives["/broken"] = nil
	request = contentRequest("value", "/broken", owner, group)
	response = ExecuteWithDependencies(request, fakeDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "tarfile is empty") {
		t.Fatalf("empty archive response = %#v", response)
	}

	fake = newCopyClient()
	fake.copyToErr = errors.New("daemon refused archive")
	request = contentRequest("value", "/new", owner, group)
	request.Force = boolPointer(true)
	response = ExecuteWithDependencies(request, fakeDependencies(fake))
	if !response.Failed || !strings.Contains(response.Msg, "daemon refused archive") {
		t.Fatalf("copy error response = %#v", response)
	}
}

func TestPathCopyStreamsLargePayloadAndNormalizesDestination(t *testing.T) {
	temp := t.TempDir()
	sourcePath := filepath.Join(temp, "payload")
	payload := bytes.Repeat([]byte("0123456789abcdef"), 32*1024)
	if err := os.WriteFile(sourcePath, payload, 0o754); err != nil {
		t.Fatal(err)
	}
	owner, group := 12, 910
	fake := newCopyClient()
	request := Request{
		Container:     "web",
		Path:          &sourcePath,
		ContainerPath: "var/lib/../data/payload",
		OwnerID:       &owner,
		GroupID:       &group,
		Force:         boolPointer(true),
	}
	response := ExecuteWithDependencies(request, fakeDependencies(fake))
	if response.Failed || response.ContainerPath != "/var/data/payload" || !bytes.Equal(fake.data, payload) {
		t.Fatalf("response = %#v, copied=%d bytes", response, len(fake.data))
	}
	if fake.header.Mode != 0o754 || fake.header.Uid != owner || fake.header.Gid != group {
		t.Fatalf("header = %#v", fake.header)
	}
}

func (fake *copyClient) addRegular(path string, content []byte, mode os.FileMode, owner, group int) {
	fake.stats[path] = apicontainer.PathStat{Name: filepath.Base(path), Size: int64(len(content)), Mode: mode}
	fake.archives[path] = makeArchive(&tar.Header{
		Name: filepath.Base(path), Mode: int64(mode.Perm()), Uid: owner, Gid: group,
		Size: int64(len(content)), Typeflag: tar.TypeReg,
	}, content)
}

func (fake *copyClient) addSymlink(path, target string, mode os.FileMode, owner, group int) {
	fake.stats[path] = apicontainer.PathStat{
		Name: filepath.Base(path), Size: int64(len(target)),
		Mode: os.ModeSymlink | mode, LinkTarget: target,
	}
	fake.archives[path] = makeArchive(&tar.Header{
		Name: filepath.Base(path), Mode: int64(mode.Perm()), Uid: owner, Gid: group,
		Typeflag: tar.TypeSymlink, Linkname: target,
	}, nil)
}

func makeArchive(header *tar.Header, content []byte) []byte {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(header); err != nil {
		panic(err)
	}
	if len(content) > 0 {
		if _, err := writer.Write(content); err != nil {
			panic(err)
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

func contentRequest(content, destination string, owner, group int) Request {
	return Request{
		Container:     "web",
		Content:       &content,
		ContainerPath: destination,
		OwnerID:       &owner,
		GroupID:       &group,
		Mode:          raw(`"0644"`),
		ModeParse:     "modern",
	}
}

func fakeDependencies(fake *copyClient) docker.Dependencies {
	return docker.Dependencies{
		NewClient:  func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
		FileSystem: docker.OSFileSystem{},
		Clock:      fixedClock{now: time.Unix(1, 0)},
	}
}

func executionState(check, diff bool) execution.State {
	return execution.State{CheckMode: check, DiffMode: diff}
}

func raw(value string) json.RawMessage { return json.RawMessage(value) }

func stringPointer(value string) *string { return &value }

func boolPointer(value bool) *bool { return &value }

func integerValue(value any) *big.Int {
	switch typed := value.(type) {
	case int:
		return big.NewInt(int64(typed))
	case *big.Int:
		return typed
	default:
		panic(fmt.Sprintf("unsupported integer %T", value))
	}
}
