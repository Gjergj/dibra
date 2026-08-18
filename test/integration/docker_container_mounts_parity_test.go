//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerContainerMountsParity independently ports the pinned
// bind, anonymous-volume, tmpfs, and mounts-plus-volumes behavior matrix.
func TestPlaybook_DockerContainerMountsParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const name = "dibra-container-mounts-parity"
	remoteExec(t, client, "docker rm -f "+name+" || true")
	defer remoteExec(t, client, "docker rm -f "+name+" || true")

	t.Run("bind ordering allow-more modes and duplicate targets", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		base := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
`
		initialArgs := base + `
      mounts:
        - source: /tmp
          target: /tmp
          type: bind
        - source: /
          target: /whatever
          type: bind
          read_only: false
`
		initial := runContainerStateTask(t, client, "mounts-bind-initial", initialArgs, "--diff")
		assertChanged(t, initial, true)
		containerID := containerResultID(t, initial)

		reordered := base + `
      mounts:
        - source: /
          target: /whatever
          type: bind
          read_only: false
        - source: /tmp
          target: /tmp
          type: bind
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-bind-reordered", reordered, "--diff"), false)

		less := base + `
      mounts:
        - source: /tmp
          target: /tmp
          type: bind
`
		lessResult := runContainerStateTask(t, client, "mounts-bind-less", less, "--diff")
		assertChanged(t, lessResult, false)
		if got := containerResultID(t, lessResult); got != containerID {
			t.Fatalf("allow-more mounts recreated container: before=%s after=%s", containerID, got)
		}

		more := base + `
      mounts:
        - source: /tmp
          target: /tmp
          type: bind
        - source: /tmp
          target: /somewhereelse
          type: bind
          read_only: true
`
		moreResult := runContainerStateTask(t, client, "mounts-bind-more", more, "--diff")
		assertChanged(t, moreResult, true)
		moreID := containerResultID(t, moreResult)
		if moreID == containerID {
			t.Fatalf("adding mount kept container ID %s", containerID)
		}

		modeChanged := strings.Replace(more, "read_only: true", "read_only: false", 1)
		modeResult := runContainerStateTask(t, client, "mounts-bind-mode", modeChanged, "--diff")
		assertChanged(t, modeResult, true)
		if got := containerResultID(t, modeResult); got == moreID {
			t.Fatalf("changing mount mode kept container ID %s", moreID)
		}

		duplicate := runContainerAgentRequest(t, client, map[string]any{
			"name": name, "image": "alpine:latest", "state": "started", "force_kill": true,
			"mounts": []any{
				map[string]any{"source": "/home", "target": "/x", "type": "bind"},
				map[string]any{"source": "/etc", "target": "/x", "type": "bind", "read_only": false},
			},
		})
		if duplicate["failed"] != true || duplicate["msg"] != `The mount point "/x" appears twice in the mounts option` {
			t.Fatalf("duplicate mount result = %#v", duplicate)
		}
	})

	t.Run("anonymous volume remains idempotent", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		args := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      mounts:
        - target: /anonymous
          type: volume
`
		created := runContainerStateTask(t, client, "mounts-anonymous", args, "--diff")
		assertChanged(t, created, true)
		containerID := containerResultID(t, created)
		source := containerMountSource(t, client, name, "/anonymous")
		if source == "" {
			t.Fatal("anonymous volume source is empty")
		}
		idempotent := runContainerStateTask(t, client, "mounts-anonymous-idempotent", args, "--diff")
		assertChanged(t, idempotent, false)
		if got := containerResultID(t, idempotent); got != containerID {
			t.Fatalf("anonymous volume run recreated container: before=%s after=%s", containerID, got)
		}
		if got := containerMountSource(t, client, name, "/anonymous"); got != source {
			t.Fatalf("anonymous volume source changed: before=%s after=%s", source, got)
		}
	})

	t.Run("tmpfs ordering mode and size", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		base := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
`
		initial := base + `
      mounts:
        - target: /cache1
          type: tmpfs
          tmpfs_mode: "1777"
          tmpfs_size: 1GB
          tmpfs_options:
            - noexec:
        - target: /cache2
          type: tmpfs
          tmpfs_mode: "1777"
          tmpfs_size: 1GB
          tmpfs_options:
            - noexec:
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-tmpfs", initial, "--diff"), true)
		reordered := base + `
      mounts:
        - target: /cache2
          type: tmpfs
          tmpfs_mode: "1777"
          tmpfs_size: 1GB
          tmpfs_options:
            - noexec:
        - target: /cache1
          type: tmpfs
          tmpfs_mode: "1777"
          tmpfs_size: 1GB
          tmpfs_options:
            - noexec:
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-tmpfs-idempotent", reordered, "--diff"), false)
		mode := base + `
      mounts:
        - target: /cache1
          type: tmpfs
          tmpfs_mode: "1700"
          tmpfs_size: 1GB
          tmpfs_options:
            - noexec:
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-tmpfs-mode", mode, "--diff"), true)
		size := strings.Replace(mode, "tmpfs_size: 1GB", "tmpfs_size: 2GB", 1)
		assertChanged(t, runContainerStateTask(t, client, "mounts-tmpfs-size", size, "--diff"), true)
		mounts := mustRemote(t, client, "docker inspect --format '{{json .Mounts}}' "+name)
		if !strings.Contains(mounts, `"Type":"tmpfs"`) || !strings.Contains(mounts, `"Destination":"/cache1"`) {
			t.Fatalf("tmpfs mount = %s", mounts)
		}
		hostMounts := mustRemote(t, client, "docker inspect --format '{{json .HostConfig.Mounts}}' "+name)
		if !strings.Contains(hostMounts, `"Options":[["noexec"]]`) {
			t.Fatalf("tmpfs host config is missing canonical options: %s", hostMounts)
		}
	})

	t.Run("advanced bind suboptions", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "rm -rf /tmp/dibra-mount-created /tmp/dibra-mount-created-force")
		args := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      mounts:
        - source: /tmp/dibra-mount-created
          target: /advanced-bind
          type: bind
          consistency: consistent
          read_only: true
          propagation: rprivate
          non_recursive: true
          create_mountpoint: true
          read_only_non_recursive: true
        - source: /tmp/dibra-mount-created-force
          target: /advanced-bind-force
          type: bind
          read_only: true
          create_mountpoint: true
          read_only_force_recursive: true
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-advanced-bind", args, "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "mounts-advanced-bind-idempotent", args, "--diff"), false)
		mustRemote(t, client, "test -d /tmp/dibra-mount-created")
		mustRemote(t, client, "test -d /tmp/dibra-mount-created-force")
		hostMounts := mustRemote(t, client, "docker inspect --format '{{json .HostConfig.Mounts}}' "+name)
		for _, expected := range []string{
			`"Consistency":"consistent"`, `"ReadOnly":true`, `"Propagation":"rprivate"`,
			`"NonRecursive":true`, `"CreateMountpoint":true`,
			`"ReadOnlyNonRecursive":true`, `"ReadOnlyForceRecursive":true`,
		} {
			if !strings.Contains(hostMounts, expected) {
				t.Fatalf("advanced bind mount is missing %s: %s", expected, hostMounts)
			}
		}
	})

	t.Run("advanced volume suboptions", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "docker volume rm -f dibra-mount-options || true")
		defer remoteExec(t, client, "docker volume rm -f dibra-mount-options || true")
		args := `
      name: ` + name + `
      image: alpine:latest
      state: present
      mounts:
        - source: dibra-mount-options
          target: /advanced-volume
          type: volume
          no_copy: true
          labels:
            purpose: parity
            enabled: true
          subpath: nested
          volume_driver: local
          volume_options:
            type: tmpfs
            device: tmpfs
            o: size=1m
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-advanced-volume", args, "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "mounts-advanced-volume-idempotent", args, "--diff"), false)
		hostMounts := mustRemote(t, client, "docker inspect --format '{{json .HostConfig.Mounts}}' "+name)
		for _, expected := range []string{
			`"NoCopy":true`, `"Labels":{"enabled":"true","purpose":"parity"}`, `"Subpath":"nested"`,
			`"Name":"local"`, `"device":"tmpfs"`, `"o":"size=1m"`, `"type":"tmpfs"`,
		} {
			if !strings.Contains(hostMounts, expected) {
				t.Fatalf("advanced volume mount is missing %s: %s", expected, hostMounts)
			}
		}
	})

	t.Run("volume subpath runtime", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		remoteExec(t, client, "docker volume rm -f dibra-mount-subpath || true")
		defer remoteExec(t, client, "docker volume rm -f dibra-mount-subpath || true")
		mustRemote(t, client, "docker volume create dibra-mount-subpath")
		mustRemote(t, client, "docker run --rm -v dibra-mount-subpath:/data alpine:latest sh -c 'mkdir -p /data/nested && printf expected > /data/nested/value'")
		args := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
      mounts:
        - source: dibra-mount-subpath
          target: /subpath
          type: volume
          subpath: nested
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-volume-subpath", args, "--diff"), true)
		if got := mustRemote(t, client, "docker exec "+name+" cat /subpath/value"); got != "expected" {
			t.Fatalf("subpath content = %q, want expected", got)
		}
		assertChanged(t, runContainerStateTask(t, client, "mounts-volume-subpath-idempotent", args, "--diff"), false)
	})

	t.Run("mounts and volumes switch and reject collisions", func(t *testing.T) {
		remoteExec(t, client, "docker rm -f "+name+" || true")
		base := `
      name: ` + name + `
      image: alpine:latest
      command: ["sleep", "600"]
      state: started
      force_kill: true
`
		initial := base + `
      mounts:
        - source: /
          target: /whatever
          type: bind
          read_only: true
      volumes:
        - /tmp:/tmp
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-volumes", initial, "--diff"), true)
		assertChanged(t, runContainerStateTask(t, client, "mounts-volumes-idempotent", initial, "--diff"), false)
		switched := base + `
      mounts:
        - source: /tmp
          target: /tmp
          type: bind
          read_only: false
      volumes:
        - /:/whatever:ro
`
		assertChanged(t, runContainerStateTask(t, client, "mounts-volumes-switch", switched, "--diff"), true)

		collision := runContainerAgentRequest(t, client, map[string]any{
			"name": name, "image": "alpine:latest", "state": "started", "force_kill": true,
			"mounts": []any{
				map[string]any{"source": "/tmp", "target": "/tmp", "type": "bind"},
			},
			"volumes": []string{"/tmp:/tmp"},
		})
		if collision["failed"] != true {
			t.Fatalf("mount/volume collision result = %#v", collision)
		}
		msg, _ := collision["msg"].(string)
		if !strings.Contains(msg, `The mount point "/tmp" appears both in the volumes and mounts option`) {
			t.Fatalf("mount/volume collision msg = %q", msg)
		}
	})
}

func containerMountSource(t *testing.T, client *ssh.Client, name, destination string) string {
	t.Helper()
	command := "docker inspect --format '{{range .Mounts}}{{if eq .Destination \"" + destination + "\"}}{{.Source}}{{end}}{{end}}' " + name
	return mustRemote(t, client, command)
}
