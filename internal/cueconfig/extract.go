package cueconfig

import (
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"

	"github.com/gjergjiramku/dibra/internal/config"
)

// moduleMapping maps CUE field names to the corresponding Go struct field setter.
// Each entry returns a function that sets the appropriate field on the Task.
var moduleMapping = map[string]func(t *config.Task, data json.RawMessage) error{
	"ping":                     setModule(func(t *config.Task) **config.PingParams { return &t.Ping }),
	"apt":                      setModule(func(t *config.Task) **config.AptParams { return &t.Apt }),
	"apt_key":                  setModule(func(t *config.Task) **config.AptKeyParams { return &t.AptKey }),
	"apt_repository":           setModule(func(t *config.Task) **config.AptRepositoryParams { return &t.AptRepository }),
	"file":                     setModule(func(t *config.Task) **config.FileParams { return &t.File }),
	"copy":                     setModule(func(t *config.Task) **config.CopyParams { return &t.Copy }),
	"fetch":                    setModule(func(t *config.Task) **config.FetchParams { return &t.Fetch }),
	"uri":                      setModule(func(t *config.Task) **config.URIParams { return &t.URI }),
	"cron":                     setModule(func(t *config.Task) **config.CronParams { return &t.Cron }),
	"ufw":                      setModule(func(t *config.Task) **config.UFWParams { return &t.UFW }),
	"user":                     setModule(func(t *config.Task) **config.UserParams { return &t.User }),
	"group":                    setModule(func(t *config.Task) **config.GroupParams { return &t.Group }),
	"systemd_service":          setModule(func(t *config.Task) **config.SystemdServiceParams { return &t.SystemdService }),
	"systemd":                  setModule(func(t *config.Task) **config.SystemdServiceParams { return &t.Systemd }),
	"service":                  setModule(func(t *config.Task) **config.ServiceParams { return &t.Service }),
	"service_facts":            setModule(func(t *config.Task) **config.ServiceFactsParams { return &t.ServiceFacts }),
	"command":                  setModule(func(t *config.Task) **config.CommandParams { return &t.Command }),
	"shell":                    setModule(func(t *config.Task) **config.ShellParams { return &t.Shell }),
	"script":                   setModule(func(t *config.Task) **config.ScriptParams { return &t.Script }),
	"unarchive":                setModule(func(t *config.Task) **config.UnarchiveParams { return &t.Unarchive }),
	"template":                 setModule(func(t *config.Task) **config.TemplateParams { return &t.Template }),
	"slurp":                    setModule(func(t *config.Task) **config.SlurpParams { return &t.Slurp }),
	"tempfile":                 setModule(func(t *config.Task) **config.TempfileParams { return &t.Tempfile }),
	"find":                     setModule(func(t *config.Task) **config.FindParams { return &t.Find }),
	"reboot":                   setModule(func(t *config.Task) **config.RebootParams { return &t.Reboot }),
	"git":                      setModule(func(t *config.Task) **config.GitParams { return &t.Git }),
	"lineinfile":               setModule(func(t *config.Task) **config.LineinfileParams { return &t.Lineinfile }),
	"blockinfile":              setModule(func(t *config.Task) **config.BlockinfileParams { return &t.Blockinfile }),
	"replace":                  setModule(func(t *config.Task) **config.ReplaceParams { return &t.Replace }),
	"iptables":                 setModule(func(t *config.Task) **config.IptablesParams { return &t.Iptables }),
	"iptables_state":           setModule(func(t *config.Task) **config.IptablesStateParams { return &t.IptablesState }),
	"docker_container":         setModule(func(t *config.Task) **config.DockerContainerParams { return &t.DockerContainer }),
	"docker_image":             setModule(func(t *config.Task) **config.DockerImageParams { return &t.DockerImage }),
	"docker_network":           setModule(func(t *config.Task) **config.DockerNetworkParams { return &t.DockerNetwork }),
	"docker_volume":            setModule(func(t *config.Task) **config.DockerVolumeParams { return &t.DockerVolume }),
	"docker_prune":             setModule(func(t *config.Task) **config.DockerPruneParams { return &t.DockerPrune }),
	"docker_login":             setModule(func(t *config.Task) **config.DockerLoginParams { return &t.DockerLogin }),
	"docker_swarm":             setModule(func(t *config.Task) **config.DockerSwarmParams { return &t.DockerSwarm }),
	"docker_swarm_service":     setModule(func(t *config.Task) **config.DockerSwarmServiceParams { return &t.DockerSwarmService }),
	"docker_node":              setModule(func(t *config.Task) **config.DockerNodeParams { return &t.DockerNode }),
	"docker_compose":           setModule(func(t *config.Task) **config.DockerComposeParams { return &t.DockerCompose }),
	"docker_compose_v2_run":    setModule(func(t *config.Task) **config.DockerComposeV2RunParams { return &t.DockerComposeV2Run }),
	"docker_secret":            setModule(func(t *config.Task) **config.DockerSecretParams { return &t.DockerSecret }),
	"docker_config":            setModule(func(t *config.Task) **config.DockerConfigParams { return &t.DockerConfig }),
	"docker_stack":             setModule(func(t *config.Task) **config.DockerStackParams { return &t.DockerStack }),
	"docker_container_exec":    setModule(func(t *config.Task) **config.DockerContainerExecParams { return &t.DockerContainerExec }),
	"docker_container_copy_into": setModule(func(t *config.Task) **config.DockerContainerCopyIntoParams { return &t.DockerContainerCopyInto }),
	"docker_image_build":       setModule(func(t *config.Task) **config.DockerImageBuildParams { return &t.DockerImageBuild }),
	"docker_image_load":        setModule(func(t *config.Task) **config.DockerImageLoadParams { return &t.DockerImageLoad }),
	"docker_image_export":      setModule(func(t *config.Task) **config.DockerImageExportParams { return &t.DockerImageExport }),
	"docker_container_info":    setModule(func(t *config.Task) **config.DockerContainerInfoParams { return &t.DockerContainerInfo }),
	"docker_image_info":        setModule(func(t *config.Task) **config.DockerImageInfoParams { return &t.DockerImageInfo }),
	"docker_network_info":      setModule(func(t *config.Task) **config.DockerNetworkInfoParams { return &t.DockerNetworkInfo }),
	"docker_volume_info":       setModule(func(t *config.Task) **config.DockerVolumeInfoParams { return &t.DockerVolumeInfo }),
	"docker_host_info":         setModule(func(t *config.Task) **config.DockerHostInfoParams { return &t.DockerHostInfo }),
	"docker_swarm_info":        setModule(func(t *config.Task) **config.DockerSwarmInfoParams { return &t.DockerSwarmInfo }),
	"docker_swarm_service_info": setModule(func(t *config.Task) **config.DockerSwarmServiceInfoParams { return &t.DockerSwarmServiceInfo }),
	"docker_node_info":         setModule(func(t *config.Task) **config.DockerNodeInfoParams { return &t.DockerNodeInfo }),
}

// setModule returns a function that JSON-decodes data into the appropriate *Params field.
func setModule[T any](fieldPtr func(t *config.Task) **T) func(t *config.Task, data json.RawMessage) error {
	return func(t *config.Task, data json.RawMessage) error {
		p := new(T)
		if len(data) > 0 && string(data) != "null" {
			if err := json.Unmarshal(data, p); err != nil {
				return fmt.Errorf("failed to decode module params: %w", err)
			}
		}
		*fieldPtr(t) = p
		return nil
	}
}

// knownTaskFields lists fields on Task that are not modules.
var knownTaskFields = map[string]bool{
	"name":     true,
	"vars":     true,
	"register": true,
}

// Extract converts a fully-evaluated CUE value into a config.Config.
func Extract(v cue.Value) (*config.Config, error) {
	cfg := &config.Config{}

	// Extract hosts
	hostsVal := v.LookupPath(cue.ParsePath("hosts"))
	if hostsVal.Exists() {
		hosts, err := extractHosts(hostsVal)
		if err != nil {
			return nil, fmt.Errorf("failed to extract hosts: %w", err)
		}
		cfg.Hosts = hosts
	}

	// Extract tasks
	tasksVal := v.LookupPath(cue.ParsePath("tasks"))
	if tasksVal.Exists() {
		tasks, err := extractTasks(tasksVal)
		if err != nil {
			return nil, fmt.Errorf("failed to extract tasks: %w", err)
		}
		cfg.Tasks = tasks
	}

	// Extract vars
	varsVal := v.LookupPath(cue.ParsePath("vars"))
	if varsVal.Exists() {
		m, err := cueToInterface(varsVal)
		if err != nil {
			return nil, fmt.Errorf("failed to extract vars: %w", err)
		}
		if mm, ok := m.(map[string]interface{}); ok {
			cfg.Vars = mm
		}
	}

	// Extract vars_files
	vfVal := v.LookupPath(cue.ParsePath("vars_files"))
	if vfVal.Exists() {
		var vf []string
		if err := vfVal.Decode(&vf); err != nil {
			return nil, fmt.Errorf("failed to extract vars_files: %w", err)
		}
		cfg.VarsFiles = vf
	}

	// Extract vars_merge
	vmVal := v.LookupPath(cue.ParsePath("vars_merge"))
	if vmVal.Exists() {
		s, err := vmVal.String()
		if err != nil {
			return nil, fmt.Errorf("failed to extract vars_merge: %w", err)
		}
		cfg.VarsMerge = s
	}

	// Extract inventory
	invVal := v.LookupPath(cue.ParsePath("inventory"))
	if invVal.Exists() {
		s, err := invVal.String()
		if err != nil {
			return nil, fmt.Errorf("failed to extract inventory: %w", err)
		}
		cfg.Inventory = s
	}

	// Apply defaults (same as config.Load)
	if cfg.VarsMerge == "" {
		cfg.VarsMerge = "replace"
	}
	for i := range cfg.Hosts {
		if cfg.Hosts[i].Port == 0 {
			cfg.Hosts[i].Port = 22
		}
	}
	for i := range cfg.Tasks {
		if cfg.Tasks[i].Apt != nil && cfg.Tasks[i].Apt.State == "" {
			cfg.Tasks[i].Apt.State = "present"
		}
	}

	return cfg, nil
}

func extractHosts(v cue.Value) ([]config.Host, error) {
	var hosts []config.Host
	iter, err := v.List()
	if err != nil {
		return nil, fmt.Errorf("hosts must be a list: %w", err)
	}
	for iter.Next() {
		var h config.Host
		data, err := iter.Value().MarshalJSON()
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &h); err != nil {
			return nil, fmt.Errorf("failed to decode host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func extractTasks(v cue.Value) ([]config.Task, error) {
	var tasks []config.Task
	iter, err := v.List()
	if err != nil {
		return nil, fmt.Errorf("tasks must be a list: %w", err)
	}
	for iter.Next() {
		t, err := extractTask(iter.Value())
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func extractTask(v cue.Value) (config.Task, error) {
	var t config.Task

	// Extract common fields
	if nameVal := v.LookupPath(cue.ParsePath("name")); nameVal.Exists() {
		s, err := nameVal.String()
		if err != nil {
			return t, fmt.Errorf("task name must be a string: %w", err)
		}
		t.Name = s
	}

	if regVal := v.LookupPath(cue.ParsePath("register")); regVal.Exists() {
		s, err := regVal.String()
		if err != nil {
			return t, fmt.Errorf("task register must be a string: %w", err)
		}
		t.Register = s
	}

	if varsVal := v.LookupPath(cue.ParsePath("vars")); varsVal.Exists() {
		m, err := cueToInterface(varsVal)
		if err != nil {
			return t, fmt.Errorf("failed to extract task vars: %w", err)
		}
		if mm, ok := m.(map[string]interface{}); ok {
			t.Vars = mm
		}
	}

	// Scan for module fields
	iter, err := v.Fields(cue.Optional(true))
	if err != nil {
		return t, fmt.Errorf("failed to iterate task fields: %w", err)
	}
	for iter.Next() {
		label := iter.Selector().String()
		if knownTaskFields[label] {
			continue
		}

		setter, ok := moduleMapping[label]
		if !ok {
			continue // ignore unknown fields (CUE schema should catch these)
		}

		data, err := iter.Value().MarshalJSON()
		if err != nil {
			return t, fmt.Errorf("failed to marshal module %q: %w", label, err)
		}
		if err := setter(&t, data); err != nil {
			return t, fmt.Errorf("failed to set module %q: %w", label, err)
		}
	}

	return t, nil
}

// cueToInterface converts a CUE value to a Go interface{} via JSON roundtrip.
func cueToInterface(v cue.Value) (interface{}, error) {
	data, err := v.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
