package docker_container_copy_into

import (
	"archive/tar"
	"context"
	"io"
	"testing"
	"time"

	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/client"
)

type copyClient struct {
	client.APIClient
	header *tar.Header
	data   []byte
}

func (fake *copyClient) CopyToContainer(_ context.Context, _ string, options client.CopyToContainerOptions) (client.CopyToContainerResult, error) {
	reader := tar.NewReader(options.Content)
	header, err := reader.Next()
	if err != nil {
		return client.CopyToContainerResult{}, err
	}
	fake.header = header
	fake.data, err = io.ReadAll(reader)
	return client.CopyToContainerResult{}, err
}

func (*copyClient) Close() error { return nil }

type fixedClock struct {
	docker.Clock
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }

func TestExecuteWithDependenciesUsesInjectedClockAndAPIClient(t *testing.T) {
	fake := &copyClient{}
	now := time.Date(2026, time.August, 11, 12, 30, 0, 0, time.UTC)
	ownerID, groupID := 1000, 1001
	response := ExecuteWithDependencies(Request{
		Container:     "web",
		Content:       "hello",
		ContainerPath: "/tmp/message.txt",
		OwnerID:       &ownerID,
		GroupID:       &groupID,
	}, docker.Dependencies{
		NewClient: func(docker.CommonArgs) (client.APIClient, error) { return fake, nil },
		Clock:     fixedClock{now: now},
	})

	if response.Failed || !response.Changed {
		t.Fatalf("ExecuteWithDependencies() = %#v", response)
	}
	if fake.header == nil || fake.header.Name != "message.txt" || !fake.header.ModTime.Equal(now) {
		t.Fatalf("tar header = %#v", fake.header)
	}
	if string(fake.data) != "hello" || fake.header.Uid != ownerID || fake.header.Gid != groupID {
		t.Errorf("tar payload = %q, uid=%d gid=%d", fake.data, fake.header.Uid, fake.header.Gid)
	}
}
