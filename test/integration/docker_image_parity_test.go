//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/gjergjiramku/dibra/internal/ssh"
)

// TestPlaybook_DockerImageParity independently ports the pinned
// community.docker docker_image integration targets (tasks/tests/basic.yml,
// options.yml, and docker_image.yml), the 5.2.2 documentation examples, and
// the combined pull/build/load/archive/tag/push contract. Register JSON is
// asserted rather than playbook CHANGED greps.
func TestPlaybook_DockerImageParity(t *testing.T) {
	client := getClient(t)
	defer client.Close()

	const (
		alpine     = "alpine:latest"
		busybox    = "busybox:latest"
		contextDir = "/tmp/dibra-docker-image-parity"
		iname      = "dibra-docker-image-parity:latest"
		aliasName  = "alpine:dibra-image-alias"
		altName    = "dibra-image-parity-hello:v1.2.3-foo"
		registry   = "dibra-docker-image-parity-registry"
		helloBase  = "127.0.0.1:5000/test/hello-world"
		testBase   = "127.0.0.1:5000/test/dibra-image-parity"
	)
	remoteExec(t, client, "docker pull "+alpine)
	remoteExec(t, client, "docker pull "+busybox)
	remoteExec(t, client, "docker rm -f "+registry+" || true")
	remoteExec(t, client, "docker rmi -f "+iname+" "+aliasName+" "+altName+" "+helloBase+":latest "+helloBase+":newtag "+helloBase+":newtag2 "+testBase+":latest "+testBase+":other || true")
	remoteExec(t, client, "rm -rf "+contextDir+" && mkdir -p "+contextDir+"/files")
	remoteExec(t, client, "rm -f /tmp/.dibra-agent /tmp/dibra-docker-image-*.json")
	defer remoteExec(t, client, "docker rm -f "+registry+" || true")
	defer remoteExec(t, client, "docker rmi -f "+iname+" "+aliasName+" "+altName+" "+helloBase+":latest "+helloBase+":newtag "+helloBase+":newtag2 "+testBase+":latest "+testBase+":other || true")
	defer remoteExec(t, client, "rm -rf "+contextDir)

	writeRemoteFile(t, client, contextDir+"/files/Dockerfile", "FROM "+busybox+"\nENV foo=/bar\nWORKDIR ${foo}\n")
	writeRemoteFile(t, client, contextDir+"/files/ArgsDockerfile", ""+
		"ARG IMAGE\nARG TEST1\nARG TEST2\nARG TEST3\n"+
		"FROM ${IMAGE}\nENV foo=/bar\nWORKDIR ${foo}\n"+
		"RUN echo \"${TEST1} - ${TEST2} - ${TEST3}\"\n")
	writeRemoteFile(t, client, contextDir+"/files/MyDockerfile", ""+
		"FROM "+alpine+"\nENV INSTALL_PATH=/newdata\nRUN mkdir -p $INSTALL_PATH\nWORKDIR $INSTALL_PATH\n")
	writeRemoteFile(t, client, contextDir+"/files/StagedDockerfile", ""+
		"FROM "+busybox+" AS first\nENV dir=/first\nWORKDIR ${dir}\n\n"+
		"FROM "+busybox+" AS second\nENV dir=/second\nWORKDIR ${dir}\n")
	writeRemoteFile(t, client, contextDir+"/files/EtcHostsDockerfile", ""+
		"FROM "+busybox+"\nRUN ping -c1 some-custom-host\n")

	removeImage := func(name string) {
		t.Helper()
		remoteExec(t, client, "docker rmi -f "+name+" || true")
	}

	t.Run("absent is idempotent", func(t *testing.T) {
		removeImage(busybox)
		defer remoteExec(t, client, "docker pull "+busybox)
		first := runDockerImage(t, client, "absent-1", `
      name: `+busybox+`
      state: absent
      force_absent: true
`)
		if first["failed"] == true {
			t.Fatalf("absent-1 = %#v", first)
		}
		second := runDockerImage(t, client, "absent-2", `
      name: `+busybox+`
      state: absent
`)
		if second["changed"] != false {
			t.Fatalf("absent-2 = %#v", second)
		}
	})

	t.Run("pull with platform is idempotent", func(t *testing.T) {
		platform := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{.Architecture}}' "+alpine))
		removeImage(alpine)
		first := runDockerImage(t, client, "pull-1", `
      name: alpine
      tag: latest
      state: present
      source: pull
      pull:
        platform: `+platform+`
`)
		if first["changed"] != true {
			t.Fatalf("pull-1 = %#v", first)
		}
		assertRawImageInspection(t, first["image"], dockerInspectImage(t, client, alpine))
		second := runDockerImage(t, client, "pull-2", `
      name: alpine
      tag: latest
      state: present
      source: pull
      pull:
        platform: `+platform+`
`)
		if second["changed"] != false {
			t.Fatalf("pull-2 = %#v", second)
		}
	})

	t.Run("tag alias force_tag and source ID", func(t *testing.T) {
		removeImage(aliasName)
		defer removeImage(aliasName)
		present := runDockerImage(t, client, "tag-source", `
      name: `+alpine+`
      state: present
      source: local
`)
		imageID, _ := present["image"].(map[string]any)["Id"].(string)
		first := runDockerImage(t, client, "tag-1", `
      source: local
      name: `+alpine+`
      repository: `+aliasName+`
`)
		if first["changed"] != true {
			t.Fatalf("tag-1 = %#v", first)
		}
		second := runDockerImage(t, client, "tag-2", `
      source: local
      name: `+alpine+`
      repository: `+aliasName+`
`)
		forced := runDockerImage(t, client, "tag-3", `
      source: local
      name: `+alpine+`
      repository: `+aliasName+`
      force_tag: true
`)
		byID := runDockerImage(t, client, "tag-4", `
      source: local
      name: `+imageID+`
      repository: `+aliasName+`
`)
		if second["changed"] != false || forced["changed"] != false || byID["changed"] != false {
			t.Fatalf("tag idempotency second=%#v force=%#v id=%#v", second, forced, byID)
		}
	})

	t.Run("image IDs are rejected for repository pull build and push", func(t *testing.T) {
		present := runDockerImage(t, client, "id-source", `
      name: `+alpine+`
      state: present
      source: local
`)
		imageID, _ := present["image"].(map[string]any)["Id"].(string)
		if output := runDockerImageOutput(t, `
      source: local
      name: `+alpine+`
      repository: `+imageID+`
`); !strings.Contains(output, "FAILED") || !strings.Contains(output, "`repository` must not be an image ID") {
			t.Fatalf("repository ID: %s", output)
		}
		if output := runDockerImageOutput(t, `
      source: local
      name: `+imageID+`
      push: true
`); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Cannot push an image by ID") {
			t.Fatalf("push ID: %s", output)
		}
		if output := runDockerImageOutput(t, `
      source: pull
      name: `+imageID+`
      force_source: true
`); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Image name must not be an image ID for source=pull") {
			t.Fatalf("pull ID: %s", output)
		}
		if output := runDockerImageOutput(t, `
      source: build
      name: `+imageID+`
      build:
        path: `+contextDir+`/files
      force_source: true
`); !strings.Contains(output, "FAILED") || !strings.Contains(output, "Image name must not be an image ID for source=build") {
			t.Fatalf("build ID: %s", output)
		}
	})

	t.Run("build args", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		args := `
      name: ` + iname + `
      source: build
      build:
        path: ` + contextDir + `/files
        dockerfile: ArgsDockerfile
        args:
          IMAGE: ` + busybox + `
          TEST1: val1
          TEST2: val2
          TEST3: "True"
        pull: false
`
		first := runDockerImage(t, client, "args-1", args)
		if first["changed"] != true {
			t.Fatalf("args-1 = %#v", first)
		}
		second := runDockerImage(t, client, "args-2", args)
		if second["changed"] != false {
			t.Fatalf("args-2 = %#v", second)
		}
	})

	t.Run("container_limits", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		tiny := runDockerImageResult(t, client, "limits-tiny", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        container_limits:
          memory: 4KB
        pull: false
`)
		ok := runDockerImage(t, client, "limits-ok", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        container_limits:
          memory: 7MB
          memswap: 8MB
        pull: false
`)
		tinyChanged := !tiny.failed && tiny.result["changed"] == true
		okChanged := ok["changed"] == true
		if tiny.failed && !strings.Contains(tiny.output, "Minimum memory limit allowed is ") &&
			!strings.Contains(strings.ToLower(tiny.output), "memory") {
			t.Fatalf("tiny limit: %s", tiny.output)
		}
		if tinyChanged == okChanged {
			t.Fatalf("expected exactly one container_limits build to change: tinyFailed=%t tiny=%#v ok=%#v", tiny.failed, tiny.result, ok)
		}
	})

	t.Run("custom dockerfile", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		result := runDockerImage(t, client, "dockerfile", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        dockerfile: MyDockerfile
        pull: false
`)
		if result["changed"] != true {
			t.Fatalf("dockerfile = %#v", result)
		}
		if !strings.Contains(resultString(result, "stdout"), "FROM "+alpine) {
			t.Fatalf("dockerfile stdout = %#v", result["stdout"])
		}
		if imageWorkingDir(t, result) != "/newdata" {
			t.Fatalf("workdir = %q", imageWorkingDir(t, result))
		}
		gotID, _ := result["image"].(map[string]any)["Id"].(string)
		if gotID != dockerInspectImage(t, client, iname)["Id"] {
			t.Fatalf("dockerfile image Id = %q", gotID)
		}
	})

	t.Run("build platform", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		args := `
      name: ` + iname + `
      source: build
      build:
        path: ` + contextDir + `/files
        platform: linux
        pull: false
`
		first := runDockerImage(t, client, "platform-1", args)
		if first["changed"] != true {
			t.Fatalf("platform-1 = %#v", first)
		}
		second := runDockerImage(t, client, "platform-2", args)
		if second["changed"] != false {
			t.Fatalf("platform-2 = %#v", second)
		}
	})

	t.Run("force_source rebuilds then is unchanged", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		runDockerImage(t, client, "force-seed", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        pull: false
`)
		first := runDockerImage(t, client, "force-1", `
      name: `+iname+`
      source: build
      force_source: true
      build:
        path: `+contextDir+`/files
        dockerfile: MyDockerfile
        pull: false
`)
		second := runDockerImage(t, client, "force-2", `
      name: `+iname+`
      source: build
      force_source: true
      build:
        path: `+contextDir+`/files
        dockerfile: MyDockerfile
        pull: false
`)
		if first["changed"] != true || second["changed"] != false {
			t.Fatalf("force_source first=%#v second=%#v", first, second)
		}
	})

	t.Run("archive and load", func(t *testing.T) {
		archive := contextDir + "/image.tar"
		mutated := contextDir + "/image_mutated.tar"
		idArchive := contextDir + "/image_id.tar"
		invalid := contextDir + "/image-invalid.tar"
		removeImage(altName)
		defer removeImage(altName)
		first := runDockerImage(t, client, "archive-1", `
      name: `+alpine+`
      archive_path: `+archive+`
      source: pull
`)
		if first["changed"] != true {
			t.Fatalf("archive-1 = %#v", first)
		}
		remoteExec(t, client, "cp "+archive+" "+mutated)
		second := runDockerImage(t, client, "archive-2", `
      name: `+alpine+`
      archive_path: `+mutated+`
      source: local
`)
		if second["failed"] == true {
			t.Fatalf("archive-2 failed: %#v", second)
		}
		// Engine 29 does not guarantee archive idempotency.
		overwrite := runDockerImage(t, client, "archive-3", `
      name: `+busybox+`
      archive_path: `+mutated+`
      source: pull
`)
		if overwrite["changed"] != true {
			t.Fatalf("archive-3 = %#v", overwrite)
		}
		remoteExec(t, client, "cp "+archive+" "+mutated)
		runDockerImage(t, client, "archive-tag", `
      name: `+alpine+`
      repository: `+altName+`
      source: local
`)
		byName := runDockerImage(t, client, "archive-4", `
      name: `+altName+`
      archive_path: `+mutated+`
      source: local
`)
		if byName["changed"] != true {
			t.Fatalf("archive-4 = %#v", byName)
		}
		imageID, _ := first["image"].(map[string]any)["Id"].(string)
		runDockerImage(t, client, "archive-id", `
      name: `+imageID+`
      archive_path: `+idArchive+`
      source: local
`)
		writeRemoteFile(t, client, invalid, "this is not a valid image")
		removeImage(alpine)
		loaded := runDockerImage(t, client, "load-1", `
      name: `+alpine+`
      load_path: `+archive+`
      source: load
`)
		if loaded["changed"] != true {
			t.Fatalf("load-1 = %#v", loaded)
		}
		if loaded["image"].(map[string]any)["Id"] != imageID {
			t.Fatalf("load id mismatch: %#v vs %s", loaded["image"], imageID)
		}
		loadedAgain := runDockerImage(t, client, "load-2", `
      name: `+alpine+`
      load_path: `+archive+`
      source: load
`)
		if loadedAgain["changed"] != false {
			t.Fatalf("load-2 = %#v", loadedAgain)
		}
		if output := runDockerImageOutput(t, `
      name: foo:bar
      load_path: `+archive+`
      source: load
`); !strings.Contains(output, "FAILED") ||
			!strings.Contains(output, "The archive did not contain image 'foo:bar'") {
			t.Fatalf("wrong name load: %s", output)
		}
		if output := runDockerImageOutput(t, `
      name: foo:bar
      load_path: `+invalid+`
      source: load
`); !strings.Contains(output, "FAILED") ||
			(!strings.Contains(output, "Detected no loaded images. Archive potentially corrupt?") &&
				!strings.Contains(output, "unexpected EOF")) {
			t.Fatalf("invalid load: %s", output)
		}
		loadedID := runDockerImage(t, client, "load-id", `
      name: `+imageID+`
      load_path: `+idArchive+`
      source: load
`)
		if loadedID["changed"] != false {
			t.Fatalf("load-id = %#v", loadedID)
		}
	})

	t.Run("build path target etc_hosts shm_size and labels", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		pathArgs := `
      name: ` + iname + `
      source: build
      build:
        path: ` + contextDir + `/files
        pull: false
`
		first := runDockerImage(t, client, "path-1", pathArgs)
		second := runDockerImage(t, client, "path-2", pathArgs)
		if first["changed"] != true || second["changed"] != false {
			t.Fatalf("path first=%#v second=%#v", first, second)
		}
		removeImage(iname)
		staged := runDockerImage(t, client, "target", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        dockerfile: StagedDockerfile
        target: first
        pull: false
`)
		if staged["changed"] != true || imageWorkingDir(t, staged) != "/first" {
			t.Fatalf("target = %#v", staged)
		}
		removeImage(iname)
		hosts := runDockerImage(t, client, "hosts", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        dockerfile: EtcHostsDockerfile
        pull: false
        etc_hosts:
          some-custom-host: "127.0.0.1"
`)
		if hosts["changed"] != true {
			t.Fatalf("etc_hosts = %#v", hosts)
		}
		removeImage(iname)
		shm := runDockerImage(t, client, "shm", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        dockerfile: MyDockerfile
        pull: false
        shm_size: 128MB
`)
		if shm["changed"] != true {
			t.Fatalf("shm_size = %#v", shm)
		}
		removeImage(iname)
		labels := runDockerImage(t, client, "labels", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        dockerfile: MyDockerfile
        pull: false
        labels:
          FOO: BAR
          "this is a label": "this is the label's value"
`)
		if labels["changed"] != true {
			t.Fatalf("labels = %#v", labels)
		}
		got := imageLabels(t, labels)
		if got["FOO"] != "BAR" || got["this is a label"] != "this is the label's value" {
			t.Fatalf("labels = %#v", got)
		}
	})

	t.Run("repository tag and local registry push pull", func(t *testing.T) {
		removeImage(iname)
		removeImage(testBase + ":latest")
		removeImage(testBase + ":other")
		removeImage(helloBase + ":latest")
		removeImage(helloBase + ":newtag")
		removeImage(helloBase + ":newtag2")
		defer removeImage(iname)
		defer removeImage(testBase + ":latest")
		defer removeImage(testBase + ":other")
		defer removeImage(helloBase + ":latest")
		defer removeImage(helloBase + ":newtag")
		defer removeImage(helloBase + ":newtag2")
		built := runDockerImage(t, client, "repo-build", `
      name: `+iname+`
      source: build
      repository: `+testBase+`
      build:
        path: `+contextDir+`/files
        pull: false
`)
		if built["changed"] != true {
			t.Fatalf("repo-build = %#v", built)
		}
		local := runDockerImage(t, client, "repo-local", `
      name: `+iname+`
      repository: `+testBase+`
      source: local
`)
		if local["changed"] != false {
			t.Fatalf("repo-local = %#v", local)
		}
		imageID, _ := built["image"].(map[string]any)["Id"].(string)
		tagged := runDockerImage(t, client, "repo-id", `
      name: `+imageID+`
      repository: `+testBase+`:other
      source: local
`)
		taggedAgain := runDockerImage(t, client, "repo-id-2", `
      name: `+imageID+`
      repository: `+testBase+`:other
      source: local
      force_tag: true
`)
		if tagged["changed"] != true || taggedAgain["changed"] != false {
			t.Fatalf("repo id tag first=%#v second=%#v", tagged, taggedAgain)
		}

		remoteExec(t, client, "docker rm -f "+registry+" || true")
		remoteExec(t, client, "docker run -d --name "+registry+" -p 5000:5000 registry:2")
		defer remoteExec(t, client, "docker rm -f "+registry+" || true")
		remoteExec(t, client, "for i in $(seq 1 30); do curl -sf http://127.0.0.1:5000/v2/ >/dev/null && break; sleep 0.2; done")

		push := runDockerImage(t, client, "push-1", `
      name: `+alpine+`
      repository: `+helloBase+`:latest
      push: true
      source: local
`)
		if push["changed"] != true {
			t.Fatalf("push-1 = %#v", push)
		}
		pushAgain := runDockerImage(t, client, "push-2", `
      name: `+alpine+`
      repository: `+helloBase+`:latest
      push: true
      source: local
`)
		pushForce := runDockerImage(t, client, "push-3", `
      name: `+alpine+`
      repository: `+helloBase+`:latest
      push: true
      source: local
      force_tag: true
`)
		if pushAgain["changed"] != false || pushForce["changed"] != false {
			t.Fatalf("push idempotency again=%#v force=%#v", pushAgain, pushForce)
		}
		removeImage(helloBase + ":latest")
		pulled := runDockerImage(t, client, "reg-pull-1", `
      name: `+helloBase+`:latest
      state: present
      source: pull
`)
		pulledAgain := runDockerImage(t, client, "reg-pull-2", `
      name: `+helloBase+`:latest
      state: present
      source: pull
`)
		if pulled["changed"] != true || pulledAgain["changed"] != false {
			t.Fatalf("registry pull first=%#v second=%#v", pulled, pulledAgain)
		}

		presentFacts := runDockerImageInfo(t, client, "facts-present", helloBase+":latest")
		if len(resultList(t, presentFacts, "images")) != 1 {
			t.Fatalf("facts present = %#v", presentFacts)
		}
		removeImage(helloBase + ":latest")
		absentFacts := runDockerImageInfo(t, client, "facts-absent", helloBase+":latest")
		if len(resultList(t, absentFacts, "images")) != 0 {
			t.Fatalf("facts absent = %#v", absentFacts)
		}
		reloaded := runDockerImage(t, client, "reg-pull-3", `
      name: `+helloBase+`:latest
      state: present
      source: pull
`)
		reloadedFacts := runDockerImageInfo(t, client, "facts-reloaded", helloBase+":latest")
		if reloaded["changed"] != true || len(resultList(t, reloadedFacts, "images")) != 1 {
			t.Fatalf("facts reload image=%#v info=%#v", reloaded, reloadedFacts)
		}

		digestRef := strings.TrimSpace(remoteExec(t, client, "docker image inspect --format '{{index .RepoDigests 0}}' "+helloBase+":latest"))
		if digestRef == "" {
			t.Fatal("missing RepoDigests after registry pull")
		}
		digestPull := runDockerImage(t, client, "reg-digest", `
      name: `+digestRef+`
      state: present
      source: pull
      force_source: true
`)
		if digestPull["changed"] != false {
			t.Fatalf("digest pull = %#v", digestPull)
		}

		taggedDifferent := runDockerImage(t, client, "tag-newtag", `
      name: `+busybox+`
      repository: `+helloBase+`:newtag
      push: false
      source: local
`)
		if taggedDifferent["failed"] == true {
			t.Fatalf("tag newtag = %#v", taggedDifferent)
		}
		pushDifferent := runDockerImage(t, client, "push-newtag-1", `
      name: `+helloBase+`
      repository: `+helloBase+`
      tag: newtag
      push: true
      source: local
`)
		pushDifferentAgain := runDockerImage(t, client, "push-newtag-2", `
      name: `+helloBase+`
      repository: `+helloBase+`
      tag: newtag
      push: true
      source: local
`)
		if pushDifferent["changed"] != true || pushDifferentAgain["changed"] != false {
			t.Fatalf("push different first=%#v second=%#v", pushDifferent, pushDifferentAgain)
		}

		runDockerImage(t, client, "tag-newtag2", `
      name: `+busybox+`
      repository: `+helloBase+`:newtag2
      push: false
      source: local
`)
		pushSame := runDockerImage(t, client, "push-same-1", `
      name: `+helloBase+`
      repository: `+helloBase+`
      tag: newtag2
      push: true
      source: local
`)
		pushSameAgain := runDockerImage(t, client, "push-same-2", `
      name: `+helloBase+`
      repository: `+helloBase+`
      tag: newtag2
      push: true
      source: local
`)
		// Docker does not report whether a new tag already existed, matching
		// the pinned upstream note in docker_image.yml.
		if pushSame["changed"] != false || pushSameAgain["changed"] != false {
			t.Fatalf("push same first=%#v second=%#v", pushSame, pushSameAgain)
		}
	})

	t.Run("check mode predicts removal without mutating", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		created := runDockerImage(t, client, "check-seed", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        pull: false
        labels:
          parity: verified
`)
		if created["changed"] != true {
			t.Fatalf("check seed = %#v", created)
		}
		check := runDockerImageWithArgs(t, client, "check-remove", `
      name: `+iname+`
      state: absent
      force_absent: true
`, "--check")
		if check["changed"] != true || check["skipped"] == true {
			t.Fatalf("check remove = %#v", check)
		}
		if inspect := remoteExec(t, client, "docker inspect "+iname+" >/dev/null && echo present"); inspect != "present" {
			t.Fatal("check mode removed the image")
		}
	})

	t.Run("docs examples archive load and build args", func(t *testing.T) {
		removeImage(iname)
		defer removeImage(iname)
		archive := contextDir + "/docs.tar"
		built := runDockerImage(t, client, "docs-build", `
      name: `+iname+`
      source: build
      build:
        path: `+contextDir+`/files
        args:
          IMAGE: `+busybox+`
        pull: false
`)
		if built["changed"] != true {
			t.Fatalf("docs build = %#v", built)
		}
		archived := runDockerImage(t, client, "docs-archive", `
      name: `+iname+`
      tag: latest
      archive_path: `+archive+`
      source: local
`)
		if archived["changed"] != true {
			t.Fatalf("docs archive = %#v", archived)
		}
		removeImage(iname)
		loaded := runDockerImage(t, client, "docs-load", `
      name: `+iname+`
      tag: latest
      load_path: `+archive+`
      source: load
`)
		if loaded["changed"] != true {
			t.Fatalf("docs load = %#v", loaded)
		}
	})
}

func runDockerImage(t *testing.T, client *ssh.Client, suffix, arguments string) map[string]any {
	t.Helper()
	run := runDockerImageResult(t, client, suffix, arguments)
	if run.failed {
		t.Fatalf("docker_image failed: %s", run.output)
	}
	return run.result
}

func runDockerImageWithArgs(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) map[string]any {
	t.Helper()
	run := runDockerImageResult(t, client, suffix, arguments, extra...)
	if run.failed {
		t.Fatalf("docker_image failed: %s", run.output)
	}
	return run.result
}

func runDockerImageOutput(t *testing.T, arguments string) string {
	t.Helper()
	return runDockerImageResult(t, nil, "fail", arguments).output
}

type dockerImageRun struct {
	output string
	result map[string]any
	failed bool
}

func runDockerImageResult(t *testing.T, client *ssh.Client, suffix, arguments string, extra ...string) dockerImageRun {
	t.Helper()
	remotePath := "/tmp/dibra-docker-image-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "image_result")
	playbook := playbookHeader + `
  - name: Manage image
    community.docker.docker_image:
` + arguments + `
    register: image_result

  - name: Persist image result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybookWithArgs(t, playbook, extra...)
	failed := strings.Contains(output, "FAILED")
	run := dockerImageRun{output: output, failed: failed}
	if failed || client == nil {
		return run
	}
	run.result = readRemoteJSONMap(t, client, remotePath)
	return run
}

func runDockerImageInfo(t *testing.T, client *ssh.Client, suffix, name string) map[string]any {
	t.Helper()
	remotePath := "/tmp/dibra-docker-image-info-" + suffix + ".json"
	templatePath := writeResultTemplate(t, "image_info_from_image")
	playbook := playbookHeader + `
  - name: Inspect image
    community.docker.docker_image_info:
      name: ` + name + `
    register: image_info_from_image

  - name: Persist image info result
    check_mode: false
    template:
      src: ` + templatePath + `
      dest: ` + remotePath + `
`
	output := runPlaybook(t, playbook)
	if strings.Contains(output, "FAILED") {
		t.Fatalf("docker_image_info failed: %s", output)
	}
	return readRemoteJSONMap(t, client, remotePath)
}
