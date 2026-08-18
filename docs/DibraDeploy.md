# dibra-deploy

`dibra-deploy` is a Linux pull runner for applying Dibra projects to the machine
on which it is running. It runs as root, uses the same standalone `dibra-agent`
as the regular controller, and does not use SSH.

## Server contract

The default endpoint and schedule are fixed in this first version:

- `GET http://localhost:8080/gettasks`
- the first request is immediate;
- later requests start 60 seconds after the previous attempt finishes;
- `200 OK` must contain one ZIP job and an `X-Dibra-Task-ID` response header;
- `204 No Content` means that no job is available;
- after processing a `200` response, the daemon sends one JSON outcome to the
  sibling `POST /gettasks_outcome` endpoint;
- an outcome has `task_id`, a `status` of `succeeded` or `failed`, an optional
  `error`, and an optional `reboot_initiated` flag;
- any 2xx outcome response acknowledges the report; a missing task ID or a
  non-2xx outcome response is an error.

For development and containerized testing, the endpoint can be overridden with
`--endpoint`. For example, a Linux container running on Docker Desktop can
poll an API on the Mac host with:

```bash
sudo ./dibra-deploy \
  --agent-path ./bin/dibra-agent \
  --endpoint http://host.docker.internal:8080/gettasks \
  --verbose
```

The repository test host publishes its port 80 as
`http://localhost:9090`, which allows HTTP-server deployment jobs to be checked
from the Docker host.

The API on macOS must listen on a non-loopback interface (for example
`0.0.0.0:8080`) so Docker Desktop can reach it. `host.docker.internal` is the
Docker Desktop hostname for the host machine. The repository's test container
also maps that name explicitly to Docker's `host-gateway`, so it works when the
Docker runtime does not inject the hostname automatically.

From this repository, the complete container setup, Linux cross-build, binary
copy, and foreground daemon run are available through:

```bash
make run-deploy-docker-host
```

The target leaves the container running after `dibra-deploy` exits. Stop it
with:

```bash
make run-deploy-docker-host-down
```

The previous `test-deploy-docker-host` and `test-deploy-docker-host-down`
target names remain available as aliases.

### Development agent versions

Local builds use the version `dev` unless a version is injected with build
flags. The local agent compatibility check compares only that version; the
embedded commit and build date are informational and do not participate in the
check. Consequently, two different local agent builds can both report `dev`.

When testing changes to the agent, use `--force-agent-upload` so an older
installed `dev` agent is not reused. `make run-deploy-docker-host` supplies this
flag automatically and therefore always installs the agent built from the
current checkout.

The server is responsible for retaining task state and excluding task IDs after
their terminal outcome is accepted. If reporting fails, the daemon logs the
error and the server may return that task again. Execution and error output is
written to stdout/stderr and is captured by journald when the sample systemd
service is used.

## Project archive

The ZIP must contain `dibra-deploy.yaml` at its root or inside one enclosing
project directory:

```yaml
version: 1
playbooks:
  - playbook.yaml
  - playbook-docker.yaml
```

Playbooks run in the listed order. Their inventory and SSH host definitions are
ignored: each playbook runs once against an implicit host named `localhost`.
Playbook variables, variable files, imported/included tasks, templates, loops,
facts, and registered results continue to work.

A deployment can contain at most one `reboot` task. It must be the final,
non-looped task in the final playbook. After the local agent initiates that
reboot, `dibra-deploy` exits successfully and systemd starts it again during the
next boot.

For safety, the daemon rejects ZIP traversal, symlinks and special files. A ZIP
is limited to 256 MiB compressed, 2 GiB expanded, and 100,000 entries.

## Install on Linux

The release installer supports Linux amd64 and arm64. It downloads the matching
`dibra-deploy` archive and `checksums.txt`, requires a valid SHA-256 checksum,
installs the binary and systemd unit, and reloads systemd. By default it does
not enable or start the service.

Download and inspect the installer before running it:

```bash
curl -fsSLo install-dibra-deploy.sh \
  https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh
less install-dibra-deploy.sh
sh install-dibra-deploy.sh
sudo systemctl enable --now dibra-deploy.service
```

For unattended installation, `--enable` enables and starts the service after
installing it:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh \
  | sh -s -- --enable
```

Pin a release when repeatable installation is required:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/Gjergj/dibra/main/scripts/install-dibra-deploy.sh \
  | sh -s -- --version v0.1.0 --enable
```

The installer invokes `sudo` when it is not already running as root. Custom
paths are available through `--install-dir` and `--unit-dir`; the generated
unit's `ExecStart` is adjusted to the selected binary path. Run
`sh install-dibra-deploy.sh --help` for all options.

The task server must be listening on the configured endpoint before the service
can receive work. The agent is not installed separately: a released
`dibra-deploy` downloads and caches its matching agent when it executes its
first job.

### Manual installation

Released builds automatically download the matching agent release and cache it
using the same policy as the controller. Development builds can select an agent:

```bash
sudo ./dibra-deploy --agent-path ./bin/dibra-agent
sudo ./dibra-deploy --agent-build
```

To install a downloaded release archive manually:

```bash
sudo install -m 0755 dibra-deploy /usr/local/bin/dibra-deploy
sudo install -m 0644 dibra-deploy.service \
  /etc/systemd/system/dibra-deploy.service
sudo systemctl daemon-reload
sudo systemctl enable --now dibra-deploy.service
```

In a source checkout, the same unit is located at
`packaging/systemd/dibra-deploy.service`.

Inspect daemon and playbook output with:

```bash
journalctl -u dibra-deploy.service -f
```

The daemon also supports `--endpoint`, `--force-agent-upload`, `--verbose`, `--version`, and
generated completion scripts through `dibra-deploy completion`.

## Integration tests

The black-box suite runs the real `dibra-deploy` and `dibra-agent` binaries as
root inside the repository's privileged Linux/systemd test container. It starts
an in-container queue server on the fixed `localhost:8080` endpoint and verifies
project execution, local assets, failure handling, archive rejection, graceful
termination, systemd state, journald output, and the reboot exit path.

```bash
# Build the container, run only dibra-deploy integration tests, then clean up
make test-deploy-integration

# Reuse an already-running integration container
make test-deploy-integration-only
```

The reboot scenario uses `/usr/local/bin/dibra-fake-reboot`, which only writes a
marker file. Do not run this suite directly on a host machine or replace that
fixture with a real reboot command.
