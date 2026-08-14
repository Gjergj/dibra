package docker_config

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
	if req.TemplateDriver != "" && req.TemplateDriver != "golang" {
		return failed(fmt.Sprintf("value of template_driver must be one of: golang, got: %s", req.TemplateDriver))
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

	manager := &configManager{
		req:            req,
		checkMode:      state.CheckMode,
		cli:            cli,
		ctx:            ctx,
		fs:             dependencies.FileSystem,
		baseName:       req.Name,
		name:           req.Name,
		rolling:        req.RollingVersions,
		templateDriver: req.TemplateDriver,
		versionsToKeep: defaultVersionsKeep,
	}
	if req.VersionsToKeep != nil {
		manager.versionsToKeep = *req.VersionsToKeep
	}
	return manager.run()
}

type configManager struct {
	req            Request
	checkMode      bool
	cli            client.APIClient
	ctx            context.Context
	fs             docker.FileSystem
	baseName       string
	name           string
	rolling        bool
	templateDriver string
	versionsToKeep int
	version        int
	data           []byte
	dataKey        string
	configs        []swarm.Config
	results        Response
}

func (m *configManager) run() Response {
	if err := m.loadData(); err != nil {
		return failed(err.Error())
	}
	if err := m.getConfigs(); err != nil {
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

func (m *configManager) loadData() error {
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
		decoded, err := decodeConfigData(*m.req.Data)
		if err != nil {
			return fmt.Errorf("Error while decoding base64 data: %v", err)
		}
		m.data = decoded
		return nil
	}
	m.data = []byte(*m.req.Data)
	return nil
}

func (m *configManager) getConfigs() error {
	filters := make(client.Filters)
	filters.Add("name", m.baseName)
	listed, err := m.cli.ConfigList(m.ctx, client.ConfigListOptions{Filters: filters})
	if err != nil {
		return fmt.Errorf("Error accessing config %s: %v", m.baseName, err)
	}
	prefix := m.baseName + "_v"
	for _, item := range listed.Items {
		if m.rolling {
			if strings.HasPrefix(item.Spec.Name, prefix) {
				m.configs = append(m.configs, item)
			}
			continue
		}
		if item.Spec.Name == m.baseName {
			m.configs = append(m.configs, item)
		}
	}
	if m.rolling {
		sort.SliceStable(m.configs, func(i, j int) bool {
			return configVersion(m.configs[i]) < configVersion(m.configs[j])
		})
	}
	return nil
}

func (m *configManager) present() error {
	if len(m.configs) > 0 {
		current := m.configs[len(m.configs)-1]
		m.results.ConfigID = current.ID
		m.results.ConfigName = current.Spec.Name
		dataChanged := false
		templateChanged := false
		labels := current.Spec.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		if existingKey, found := labels[ansibleKeyLabel]; found {
			if existingKey != m.dataKey {
				dataChanged = true
			}
		}
		existingTemplate := ""
		if current.Spec.Templating != nil {
			existingTemplate = current.Spec.Templating.Name
		}
		if existingTemplate != "" {
			if existingTemplate != m.templateDriver {
				templateChanged = true
			}
		} else if m.templateDriver != "" {
			templateChanged = true
		}
		labelsChanged := !labelsAllowMorePresent(m.req.Labels, labels)
		if m.rolling {
			m.version = configVersion(current)
		}
		if dataChanged || templateChanged || labelsChanged || m.req.Force {
			if !m.rolling {
				if err := m.absent(); err != nil {
					return err
				}
			}
			id, err := m.createConfig()
			if err != nil {
				return err
			}
			m.results.Changed = true
			m.results.ConfigID = id
			m.results.ConfigName = m.name
		}
		return nil
	}
	m.results.Changed = true
	id, err := m.createConfig()
	if err != nil {
		return err
	}
	m.results.ConfigID = id
	m.results.ConfigName = m.name
	return nil
}

func (m *configManager) createConfig() (string, error) {
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
	spec := swarm.ConfigSpec{
		Annotations: swarm.Annotations{
			Name:   m.name,
			Labels: labels,
		},
		Data: m.data,
	}
	if m.templateDriver != "" {
		spec.Templating = &swarm.Driver{Name: m.templateDriver}
	}
	created, err := m.cli.ConfigCreate(m.ctx, client.ConfigCreateOptions{Spec: spec})
	if err != nil {
		return "", fmt.Errorf("Error creating config: %v", err)
	}
	idFilters := make(client.Filters)
	idFilters.Add("id", created.ID)
	listed, err := m.cli.ConfigList(m.ctx, client.ConfigListOptions{Filters: idFilters})
	if err == nil && len(listed.Items) > 0 {
		m.configs = append(m.configs, listed.Items...)
	} else {
		m.configs = append(m.configs, swarm.Config{
			ID:   created.ID,
			Spec: spec,
		})
	}
	return created.ID, nil
}

func (m *configManager) removeOldVersions() error {
	if !m.rolling || m.versionsToKeep < 0 || m.checkMode {
		return nil
	}
	keep := m.versionsToKeep
	if keep < 1 {
		keep = 1
	}
	for len(m.configs) > keep {
		oldest := m.configs[0]
		m.configs = m.configs[1:]
		if err := m.removeConfig(oldest); err != nil {
			return err
		}
	}
	return nil
}

func (m *configManager) absent() error {
	if len(m.configs) == 0 {
		return nil
	}
	for _, item := range m.configs {
		if err := m.removeConfig(item); err != nil {
			return err
		}
	}
	m.configs = nil
	m.results.Changed = true
	return nil
}

func (m *configManager) removeConfig(item swarm.Config) error {
	if m.checkMode {
		return nil
	}
	if _, err := m.cli.ConfigRemove(m.ctx, item.ID, client.ConfigRemoveOptions{}); err != nil {
		return fmt.Errorf("Error removing config %s: %v", item.Spec.Name, err)
	}
	return nil
}

func configVersion(item swarm.Config) int {
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

func decodeConfigData(value string) ([]byte, error) {
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
