package docker_secret

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gjergjiramku/dibra/internal/execution"
	"github.com/gjergjiramku/dibra/internal/modules/docker"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func Execute(req Request) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, execution.State{})
}

func ExecuteWithState(req Request, state execution.State) Response {
	return ExecuteWithDependenciesAndState(req, docker.Dependencies{}, state)
}

func ExecuteWithDependencies(req Request, dependencies docker.Dependencies) Response {
	return ExecuteWithDependenciesAndState(req, dependencies, execution.State{})
}

func ExecuteWithDependenciesAndState(req Request, dependencies docker.Dependencies, state execution.State) Response {
	dependencies = dependencies.Resolve()
	if strings.TrimSpace(req.Name) == "" {
		return failed("missing required arguments: name")
	}
	stateName := req.State
	if stateName == "" {
		stateName = "present"
	}
	switch stateName {
	case "present", "absent":
	default:
		return failed(fmt.Sprintf("value of state must be one of: absent, present, got: %s", stateName))
	}
	if req.Data != nil && req.DataSrc != "" {
		return failed("parameters are mutually exclusive: data, data_src")
	}
	if stateName == "present" && req.Data == nil && req.DataSrc == "" {
		return failed("state is present but any of the following are missing: data, data_src")
	}

	cli, err := dependencies.NewClient(req.CommonArgs)
	if err != nil {
		return failed(fmt.Sprintf("An unexpected Docker error occurred: %v", err))
	}
	defer cli.Close()

	ctx, cancel := docker.GetContextWithEnvironment(req.CommonArgs, dependencies.Environment)
	defer cancel()

	manager := &secretManager{
		req:            req,
		checkMode:      state.CheckMode,
		cli:            cli,
		ctx:            ctx,
		fs:             dependencies.FileSystem,
		baseName:       req.Name,
		name:           req.Name,
		rolling:        req.RollingVersions,
		versionsToKeep: defaultVersionsKeep,
	}
	if req.VersionsToKeep != nil {
		manager.versionsToKeep = *req.VersionsToKeep
	}
	return manager.run()
}

type secretManager struct {
	req            Request
	checkMode      bool
	cli            client.APIClient
	ctx            context.Context
	fs             docker.FileSystem
	baseName       string
	name           string
	rolling        bool
	versionsToKeep int
	version        int
	data           []byte
	dataKey        string
	secrets        []swarm.Secret
	results        Response
}

func (m *secretManager) run() Response {
	if err := m.loadData(); err != nil {
		return failed(err.Error())
	}
	if err := m.getSecrets(); err != nil {
		return failed(err.Error())
	}
	switch m.req.State {
	case "", "present":
		m.dataKey = sha224Hex(m.data)
		if err := m.present(); err != nil {
			return failed(err.Error())
		}
		if err := m.removeOldVersions(); err != nil {
			return failed(err.Error())
		}
	case "absent":
		if err := m.absent(); err != nil {
			return failed(err.Error())
		}
	}
	return m.results
}

func (m *secretManager) loadData() error {
	if m.req.State == "absent" && m.req.Data == nil && m.req.DataSrc == "" {
		return nil
	}
	if m.req.DataSrc != "" {
		raw, err := m.fs.ReadFile(m.req.DataSrc)
		if err != nil {
			return fmt.Errorf("Error while reading %s: %v", m.req.DataSrc, err)
		}
		m.data = raw
		return nil
	}
	if m.req.Data == nil {
		return nil
	}
	if m.req.DataIsB64 {
		decoded, err := decodeSecretData(*m.req.Data)
		if err != nil {
			return fmt.Errorf("Error while decoding base64 data: %v", err)
		}
		m.data = decoded
		return nil
	}
	m.data = []byte(*m.req.Data)
	return nil
}

func (m *secretManager) getSecrets() error {
	filters := make(client.Filters)
	filters.Add("name", m.baseName)
	listed, err := m.cli.SecretList(m.ctx, client.SecretListOptions{Filters: filters})
	if err != nil {
		return fmt.Errorf("Error accessing secret %s: %v", m.baseName, err)
	}
	prefix := m.baseName + "_v"
	for _, item := range listed.Items {
		if m.rolling {
			if strings.HasPrefix(item.Spec.Name, prefix) {
				m.secrets = append(m.secrets, item)
			}
			continue
		}
		if item.Spec.Name == m.baseName {
			m.secrets = append(m.secrets, item)
		}
	}
	if m.rolling {
		sort.SliceStable(m.secrets, func(i, j int) bool {
			return secretVersion(m.secrets[i]) < secretVersion(m.secrets[j])
		})
	}
	return nil
}

func (m *secretManager) present() error {
	if len(m.secrets) > 0 {
		current := m.secrets[len(m.secrets)-1]
		m.results.SecretID = current.ID
		m.results.SecretName = current.Spec.Name
		dataChanged := false
		labels := current.Spec.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		if existingKey, found := labels[ansibleKeyLabel]; found {
			if existingKey != m.dataKey {
				dataChanged = true
			}
		}
		labelsChanged := !labelsAllowMorePresent(m.req.Labels, labels)
		if m.rolling {
			m.version = secretVersion(current)
		}
		if dataChanged || labelsChanged || m.req.Force {
			if !m.rolling {
				if err := m.absent(); err != nil {
					return err
				}
			}
			id, err := m.createSecret()
			if err != nil {
				return err
			}
			m.results.Changed = true
			m.results.SecretID = id
			m.results.SecretName = m.name
		}
		return nil
	}
	m.results.Changed = true
	id, err := m.createSecret()
	if err != nil {
		return err
	}
	m.results.SecretID = id
	m.results.SecretName = m.name
	return nil
}

func (m *secretManager) createSecret() (string, error) {
	labels := map[string]string{ansibleKeyLabel: m.dataKey}
	if m.rolling {
		m.version++
		labels[ansibleVersionLabel] = strconv.Itoa(m.version)
		m.name = fmt.Sprintf("%s_v%d", m.baseName, m.version)
	}
	for key, value := range m.req.Labels {
		labels[key] = value
	}
	if m.checkMode {
		return "", nil
	}
	spec := swarm.SecretSpec{
		Annotations: swarm.Annotations{
			Name:   m.name,
			Labels: labels,
		},
		Data: m.data,
	}
	created, err := m.cli.SecretCreate(m.ctx, client.SecretCreateOptions{Spec: spec})
	if err != nil {
		return "", fmt.Errorf("Error creating secret: %v", err)
	}
	idFilters := make(client.Filters)
	idFilters.Add("id", created.ID)
	listed, err := m.cli.SecretList(m.ctx, client.SecretListOptions{Filters: idFilters})
	if err == nil && len(listed.Items) > 0 {
		m.secrets = append(m.secrets, listed.Items...)
	} else {
		m.secrets = append(m.secrets, swarm.Secret{
			ID:   created.ID,
			Spec: spec,
		})
	}
	return created.ID, nil
}

func (m *secretManager) removeOldVersions() error {
	if !m.rolling || m.versionsToKeep < 0 || m.checkMode {
		return nil
	}
	keep := m.versionsToKeep
	if keep < 1 {
		keep = 1
	}
	for len(m.secrets) > keep {
		oldest := m.secrets[0]
		m.secrets = m.secrets[1:]
		if err := m.removeSecret(oldest); err != nil {
			return err
		}
	}
	return nil
}

func (m *secretManager) absent() error {
	if len(m.secrets) == 0 {
		return nil
	}
	for _, item := range m.secrets {
		if err := m.removeSecret(item); err != nil {
			return err
		}
	}
	m.secrets = nil
	m.results.Changed = true
	return nil
}

func (m *secretManager) removeSecret(item swarm.Secret) error {
	if m.checkMode {
		return nil
	}
	if _, err := m.cli.SecretRemove(m.ctx, item.ID, client.SecretRemoveOptions{}); err != nil {
		return fmt.Errorf("Error removing secret %s: %v", item.Spec.Name, err)
	}
	return nil
}

func secretVersion(item swarm.Secret) int {
	if item.Spec.Labels == nil {
		return 0
	}
	raw, ok := item.Spec.Labels[ansibleVersionLabel]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

func labelsAllowMorePresent(desired LabelMap, current map[string]string) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		current = map[string]string{}
	}
	for key, value := range desired {
		existing, found := current[key]
		if !found || existing != value {
			return false
		}
	}
	return true
}

func sha224Hex(data []byte) string {
	sum := sha256.Sum224(data)
	return hex.EncodeToString(sum[:])
}

func decodeSecretData(value string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	decoded, err := base64.URLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func failed(msg string) Response {
	return Response{Failed: true, Msg: msg}
}
