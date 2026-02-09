package config

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Hosts     []Host                 `yaml:"hosts"`
	Tasks     []Task                 `yaml:"tasks"`
	Vars      map[string]interface{} `yaml:"vars,omitempty"`
	VarsFiles []string               `yaml:"vars_files,omitempty"`
	VarsMerge string                 `yaml:"vars_merge,omitempty"`
}

type Host struct {
	Name           string   `yaml:"name"`
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	User           string   `yaml:"user"`
	Password       string   `yaml:"password"`
	SSHKeyPath     string   `yaml:"ssh_key_path"`
	Become         bool     `yaml:"become"`
	BecomePassword string   `yaml:"become_password"`
	Groups         []string `yaml:"groups,omitempty"`
}

type ImportTasksParams struct {
	File string `yaml:"file,omitempty"`
}

func (p *ImportTasksParams) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.File = node.Value
		return nil
	}
	type alias ImportTasksParams
	var a alias
	if err := node.Decode(&a); err != nil {
		return err
	}
	*p = ImportTasksParams(a)
	return nil
}

type IncludeTasksParams struct {
	File string `yaml:"file,omitempty"`
}

func (p *IncludeTasksParams) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		p.File = node.Value
		return nil
	}
	type alias IncludeTasksParams
	var a alias
	if err := node.Decode(&a); err != nil {
		return err
	}
	*p = IncludeTasksParams(a)
	return nil
}

type Task struct {
	Name                    string                         `yaml:"name"`
	Vars                    map[string]interface{}         `yaml:"vars,omitempty"`
	ImportTasks             *ImportTasksParams             `yaml:"import_tasks,omitempty"`
	IncludeTasks            *IncludeTasksParams            `yaml:"include_tasks,omitempty"`
	SourceDir               string                         `yaml:"-"`
	Apt                     *AptParams                     `yaml:"apt,omitempty"`
	AptKey                  *AptKeyParams                  `yaml:"apt_key,omitempty"`
	AptRepository           *AptRepositoryParams           `yaml:"apt_repository,omitempty"`
	File                    *FileParams                    `yaml:"file,omitempty"`
	Copy                    *CopyParams                    `yaml:"copy,omitempty"`
	Fetch                   *FetchParams                   `yaml:"fetch,omitempty"`
	URI                     *URIParams                     `yaml:"uri,omitempty"`
	Cron                    *CronParams                    `yaml:"cron,omitempty"`
	UFW                     *UFWParams                     `yaml:"ufw,omitempty"`
	User                    *UserParams                    `yaml:"user,omitempty"`
	Group                   *GroupParams                   `yaml:"group,omitempty"`
	SystemdService          *SystemdServiceParams          `yaml:"systemd_service,omitempty"`
	Systemd                 *SystemdServiceParams          `yaml:"systemd,omitempty"`
	Service                 *ServiceParams                 `yaml:"service,omitempty"`
	ServiceFacts            *ServiceFactsParams            `yaml:"service_facts,omitempty"`
	Ping                    *PingParams                    `yaml:"ping,omitempty"`
	Slurp                   *SlurpParams                   `yaml:"slurp,omitempty"`
	Command                 *CommandParams                 `yaml:"command,omitempty"`
	Shell                   *ShellParams                   `yaml:"shell,omitempty"`
	Script                  *ScriptParams                  `yaml:"script,omitempty"`
	Unarchive               *UnarchiveParams               `yaml:"unarchive,omitempty"`
	Git                     *GitParams                     `yaml:"git,omitempty"`
	Lineinfile              *LineinfileParams              `yaml:"lineinfile,omitempty"`
	Blockinfile             *BlockinfileParams             `yaml:"blockinfile,omitempty"`
	Replace                 *ReplaceParams                 `yaml:"replace,omitempty"`
	Iptables                *IptablesParams                `yaml:"iptables,omitempty"`
	IptablesState           *IptablesStateParams           `yaml:"iptables_state,omitempty"`
	Tempfile                *TempfileParams                `yaml:"tempfile,omitempty"`
	Reboot                  *RebootParams                  `yaml:"reboot,omitempty"`
	DockerContainer         *DockerContainerParams         `yaml:"docker_container,omitempty"`
	DockerImage             *DockerImageParams             `yaml:"docker_image,omitempty"`
	DockerNetwork           *DockerNetworkParams           `yaml:"docker_network,omitempty"`
	DockerVolume            *DockerVolumeParams            `yaml:"docker_volume,omitempty"`
	DockerPrune             *DockerPruneParams             `yaml:"docker_prune,omitempty"`
	DockerLogin             *DockerLoginParams             `yaml:"docker_login,omitempty"`
	DockerSwarm             *DockerSwarmParams             `yaml:"docker_swarm,omitempty"`
	DockerSwarmService      *DockerSwarmServiceParams      `yaml:"docker_swarm_service,omitempty"`
	DockerNode              *DockerNodeParams              `yaml:"docker_node,omitempty"`
	DockerCompose           *DockerComposeParams           `yaml:"docker_compose,omitempty"`
	DockerSecret            *DockerSecretParams            `yaml:"docker_secret,omitempty"`
	DockerConfig            *DockerConfigParams            `yaml:"docker_config,omitempty"`
	DockerStack             *DockerStackParams             `yaml:"docker_stack,omitempty"`
	DockerContainerExec     *DockerContainerExecParams     `yaml:"docker_container_exec,omitempty"`
	DockerContainerCopyInto *DockerContainerCopyIntoParams `yaml:"docker_container_copy_into,omitempty"`
	DockerImageBuild        *DockerImageBuildParams        `yaml:"docker_image_build,omitempty"`
	DockerImageLoad         *DockerImageLoadParams         `yaml:"docker_image_load,omitempty"`
	DockerImageExport       *DockerImageExportParams       `yaml:"docker_image_export,omitempty"`
	DockerComposeV2Run      *DockerComposeV2RunParams      `yaml:"docker_compose_v2_run,omitempty"`
	DockerContainerInfo     *DockerContainerInfoParams     `yaml:"docker_container_info,omitempty"`
	DockerImageInfo         *DockerImageInfoParams         `yaml:"docker_image_info,omitempty"`
	DockerNetworkInfo       *DockerNetworkInfoParams       `yaml:"docker_network_info,omitempty"`
	DockerVolumeInfo        *DockerVolumeInfoParams        `yaml:"docker_volume_info,omitempty"`
	DockerHostInfo          *DockerHostInfoParams          `yaml:"docker_host_info,omitempty"`
	DockerSwarmInfo         *DockerSwarmInfoParams         `yaml:"docker_swarm_info,omitempty"`
	DockerSwarmServiceInfo  *DockerSwarmServiceInfoParams  `yaml:"docker_swarm_service_info,omitempty"`
	DockerNodeInfo          *DockerNodeInfoParams          `yaml:"docker_node_info,omitempty"`
}

type DockerSwarmServiceParams struct {
	Name          string            `yaml:"name"`
	Image         string            `yaml:"image"`
	State         string            `yaml:"state,omitempty"`
	Replicas      *uint64           `yaml:"replicas,omitempty"`
	Args          []string          `yaml:"args,omitempty"`
	Command       interface{}       `yaml:"command,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Publish       []PortPublish     `yaml:"publish,omitempty"`
	Networks      []string          `yaml:"networks,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	LimitCPU      float64           `yaml:"limit_cpu,omitempty"`
	LimitMemory   int64             `yaml:"limit_memory,omitempty"`
	Constraint    []string          `yaml:"constraint,omitempty"`
	RestartPolicy string            `yaml:"restart_policy,omitempty"`
	ForceUpdate   bool              `yaml:"force_update,omitempty"`

	// Phase 6.1: Configs and Secrets
	Configs []ServiceConfigReference `yaml:"configs,omitempty"`
	Secrets []ServiceSecretReference `yaml:"secrets,omitempty"`

	// Phase 6.2: Update and Rollback Configuration
	UpdateDelay             string  `yaml:"update_delay,omitempty"`
	UpdateParallelism       uint64  `yaml:"update_parallelism,omitempty"`
	UpdateFailureAction     string  `yaml:"update_failure_action,omitempty"`
	UpdateOrder             string  `yaml:"update_order,omitempty"`
	UpdateMonitor           string  `yaml:"update_monitor,omitempty"`
	MaxFailureRatio         float32 `yaml:"max_failure_ratio,omitempty"`
	RollbackDelay           string  `yaml:"rollback_delay,omitempty"`
	RollbackParallelism     uint64  `yaml:"rollback_parallelism,omitempty"`
	RollbackFailureAction   string  `yaml:"rollback_failure_action,omitempty"`
	RollbackOrder           string  `yaml:"rollback_order,omitempty"`
	RollbackMonitor         string  `yaml:"rollback_monitor,omitempty"`
	RollbackMaxFailureRatio float32 `yaml:"rollback_max_failure_ratio,omitempty"`

	// Phase 6.3: Additional Options
	Healthcheck *ServiceHealthcheckParams `yaml:"healthcheck,omitempty"`
	DNS         []string                  `yaml:"dns,omitempty"`
	DNSSearch   []string                  `yaml:"dns_search,omitempty"`
	DNSOptions  []string                  `yaml:"dns_options,omitempty"`
	Hosts       []string                  `yaml:"hosts,omitempty"`
	Mounts      []ServiceMountParams      `yaml:"mounts,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type ServiceConfigReference struct {
	ConfigName string `yaml:"config_name"`
	ConfigID   string `yaml:"config_id,omitempty"`
	FileName   string `yaml:"filename,omitempty"`
	UID        string `yaml:"uid,omitempty"`
	GID        string `yaml:"gid,omitempty"`
	Mode       uint32 `yaml:"mode,omitempty"`
}

type ServiceSecretReference struct {
	SecretName string `yaml:"secret_name"`
	SecretID   string `yaml:"secret_id,omitempty"`
	FileName   string `yaml:"filename,omitempty"`
	UID        string `yaml:"uid,omitempty"`
	GID        string `yaml:"gid,omitempty"`
	Mode       uint32 `yaml:"mode,omitempty"`
}

type ServiceHealthcheckParams struct {
	Test        []string `yaml:"test,omitempty"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

type ServiceMountParams struct {
	Type            string            `yaml:"type,omitempty"`
	Source          string            `yaml:"source,omitempty"`
	Target          string            `yaml:"target,omitempty"`
	ReadOnly        bool              `yaml:"read_only,omitempty"`
	Consistency     string            `yaml:"consistency,omitempty"`
	BindPropagation string            `yaml:"bind_propagation,omitempty"`
	VolumeDriver    string            `yaml:"volume_driver,omitempty"`
	VolumeLabels    map[string]string `yaml:"volume_labels,omitempty"`
	VolumeOptions   map[string]string `yaml:"volume_options,omitempty"`
	VolumeNoCopy    bool              `yaml:"volume_no_copy,omitempty"`
	TmpfsSize       int64             `yaml:"tmpfs_size,omitempty"`
	TmpfsMode       uint32            `yaml:"tmpfs_mode,omitempty"`
}

type PortPublish struct {
	PublishedPort uint32 `yaml:"published_port"`
	TargetPort    uint32 `yaml:"target_port"`
	Protocol      string `yaml:"protocol,omitempty"`
	Mode          string `yaml:"mode,omitempty"`
}

type DockerNodeParams struct {
	Hostname       string            `yaml:"hostname,omitempty"`
	Self           bool              `yaml:"self,omitempty"`
	Availability   string            `yaml:"availability,omitempty"`
	Role           string            `yaml:"role,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
	LabelsState    string            `yaml:"labels_state,omitempty"`
	LabelsToRemove []string          `yaml:"labels_to_remove,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerSwarmServiceInfoParams struct {
	Name       string `yaml:"name"`
	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerNodeInfoParams struct {
	Name       string `yaml:"name,omitempty"`
	Self       bool   `yaml:"self,omitempty"`
	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type ServiceFactsParams struct {
}

type TempfileParams struct {
	Path   string  `yaml:"path,omitempty"`
	Prefix *string `yaml:"prefix,omitempty"`
	Suffix string  `yaml:"suffix,omitempty"`
	State  string  `yaml:"state,omitempty"`
}

type RebootParams struct {
	PreRebootDelay  int      `yaml:"pre_reboot_delay,omitempty"`
	PostRebootDelay int      `yaml:"post_reboot_delay,omitempty"`
	RebootTimeout   int      `yaml:"reboot_timeout,omitempty"`
	ConnectTimeout  int      `yaml:"connect_timeout,omitempty"`
	TestCommand     string   `yaml:"test_command,omitempty"`
	Msg             string   `yaml:"msg,omitempty"`
	SearchPaths     []string `yaml:"search_paths,omitempty"`
	BootTimeCommand string   `yaml:"boot_time_command,omitempty"`
	RebootCommand   string   `yaml:"reboot_command,omitempty"`
}

type PingParams struct {
	Data string `yaml:"data,omitempty"`
}

type SlurpParams struct {
	Src  string `yaml:"src,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type CommandParams struct {
	Cmd             string   `yaml:"cmd,omitempty"`
	Argv            []string `yaml:"argv,omitempty"`
	Chdir           string   `yaml:"chdir,omitempty"`
	Creates         string   `yaml:"creates,omitempty"`
	Removes         string   `yaml:"removes,omitempty"`
	Stdin           string   `yaml:"stdin,omitempty"`
	StdinAddNewline *bool    `yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool    `yaml:"strip_empty_ends,omitempty"`
}

type ShellParams struct {
	Cmd             string `yaml:"cmd,omitempty"`
	Chdir           string `yaml:"chdir,omitempty"`
	Creates         string `yaml:"creates,omitempty"`
	Removes         string `yaml:"removes,omitempty"`
	Stdin           string `yaml:"stdin,omitempty"`
	StdinAddNewline *bool  `yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool  `yaml:"strip_empty_ends,omitempty"`
	Executable      string `yaml:"executable,omitempty"`
}

type ScriptParams struct {
	Cmd            string `yaml:"cmd,omitempty"`
	Chdir          string `yaml:"chdir,omitempty"`
	Creates        string `yaml:"creates,omitempty"`
	Removes        string `yaml:"removes,omitempty"`
	Executable     string `yaml:"executable,omitempty"`
	StripEmptyEnds *bool  `yaml:"strip_empty_ends,omitempty"`
}

type CopyParams struct {
	Src       string `yaml:"src,omitempty"`
	Dest      string `yaml:"dest"`
	Content   string `yaml:"content,omitempty"`
	Mode      string `yaml:"mode,omitempty"`
	Owner     string `yaml:"owner,omitempty"`
	Group     string `yaml:"group,omitempty"`
	Backup    bool   `yaml:"backup"`
	Force     bool   `yaml:"force"`
	RemoteSrc bool   `yaml:"remote_src"`
}

type UnarchiveParams struct {
	Src       string   `yaml:"src"`
	Dest      string   `yaml:"dest"`
	RemoteSrc bool     `yaml:"remote_src"`
	Creates   string   `yaml:"creates,omitempty"`
	ListFiles bool     `yaml:"list_files"`
	Exclude   []string `yaml:"exclude,omitempty"`
	Include   []string `yaml:"include,omitempty"`
	KeepNewer bool     `yaml:"keep_newer"`
	ExtraOpts []string `yaml:"extra_opts,omitempty"`
	Mode      string   `yaml:"mode,omitempty"`
	Owner     string   `yaml:"owner,omitempty"`
	Group     string   `yaml:"group,omitempty"`
}

type FetchParams struct {
	Src              string `yaml:"src"`
	Dest             string `yaml:"dest"`
	Flat             bool   `yaml:"flat"`
	FailOnMissing    *bool  `yaml:"fail_on_missing,omitempty"`
	ValidateChecksum *bool  `yaml:"validate_checksum,omitempty"`
}

type URIParams struct {
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method,omitempty"`
	Body            string            `yaml:"body,omitempty"`
	BodyFormat      string            `yaml:"body_format,omitempty"`
	Headers         map[string]string `yaml:"headers,omitempty"`
	StatusCode      []int             `yaml:"status_code,omitempty"`
	Timeout         int               `yaml:"timeout,omitempty"`
	ReturnContent   bool              `yaml:"return_content"`
	Dest            string            `yaml:"dest,omitempty"`
	Creates         string            `yaml:"creates,omitempty"`
	URLUsername     string            `yaml:"url_username,omitempty"`
	URLPassword     string            `yaml:"url_password,omitempty"`
	ForceBasicAuth  bool              `yaml:"force_basic_auth"`
	FollowRedirects string            `yaml:"follow_redirects,omitempty"`
	ValidateCerts   *bool             `yaml:"validate_certs,omitempty"`
}

type CronParams struct {
	Name         string `yaml:"name"`
	User         string `yaml:"user,omitempty"`
	Job          string `yaml:"job,omitempty"`
	State        string `yaml:"state,omitempty"`
	Minute       string `yaml:"minute,omitempty"`
	Hour         string `yaml:"hour,omitempty"`
	Day          string `yaml:"day,omitempty"`
	Month        string `yaml:"month,omitempty"`
	Weekday      string `yaml:"weekday,omitempty"`
	SpecialTime  string `yaml:"special_time,omitempty"`
	Disabled     bool   `yaml:"disabled"`
	Backup       bool   `yaml:"backup"`
	CronFile     string `yaml:"cron_file,omitempty"`
	Env          bool   `yaml:"env"`
	InsertAfter  string `yaml:"insertafter,omitempty"`
	InsertBefore string `yaml:"insertbefore,omitempty"`
}

type UFWParams struct {
	State            string `yaml:"state,omitempty"`
	Logging          string `yaml:"logging,omitempty"`
	Default          string `yaml:"default,omitempty"`
	Policy           string `yaml:"policy,omitempty"`
	Direction        string `yaml:"direction,omitempty"`
	Rule             string `yaml:"rule,omitempty"`
	Delete           bool   `yaml:"delete"`
	Insert           int    `yaml:"insert,omitempty"`
	InsertRelativeTo string `yaml:"insert_relative_to,omitempty"`
	Interface        string `yaml:"interface,omitempty"`
	If               string `yaml:"if,omitempty"`
	InterfaceIn      string `yaml:"interface_in,omitempty"`
	IfIn             string `yaml:"if_in,omitempty"`
	InterfaceOut     string `yaml:"interface_out,omitempty"`
	IfOut            string `yaml:"if_out,omitempty"`
	FromIP           string `yaml:"from_ip,omitempty"`
	From             string `yaml:"from,omitempty"`
	Src              string `yaml:"src,omitempty"`
	FromPort         string `yaml:"from_port,omitempty"`
	ToIP             string `yaml:"to_ip,omitempty"`
	Dest             string `yaml:"dest,omitempty"`
	To               string `yaml:"to,omitempty"`
	ToPort           string `yaml:"to_port,omitempty"`
	Port             string `yaml:"port,omitempty"`
	Proto            string `yaml:"proto,omitempty"`
	Protocol         string `yaml:"protocol,omitempty"`
	Name             string `yaml:"name,omitempty"`
	App              string `yaml:"app,omitempty"`
	Route            bool   `yaml:"route"`
	Log              bool   `yaml:"log"`
	Comment          string `yaml:"comment,omitempty"`
}

type FileParams struct {
	Path    string `yaml:"path"`
	State   string `yaml:"state,omitempty"`
	Mode    string `yaml:"mode,omitempty"`
	Owner   string `yaml:"owner,omitempty"`
	Group   string `yaml:"group,omitempty"`
	Src     string `yaml:"src,omitempty"`
	Recurse bool   `yaml:"recurse"`
	Force   bool   `yaml:"force"`
	Follow  bool   `yaml:"follow"`
}

type AptKeyParams struct {
	URL     string `yaml:"url,omitempty"`
	Data    string `yaml:"data,omitempty"`
	File    string `yaml:"file,omitempty"`
	Keyring string `yaml:"keyring,omitempty"`
	ID      string `yaml:"id,omitempty"`
	State   string `yaml:"state,omitempty"`
}

type AptRepositoryParams struct {
	Repo        string `yaml:"repo"`
	State       string `yaml:"state,omitempty"`
	Filename    string `yaml:"filename,omitempty"`
	UpdateCache bool   `yaml:"update_cache"`
}

type AptParams struct {
	Name           interface{} `yaml:"name"`
	State          string      `yaml:"state"`
	UpdateCache    bool        `yaml:"update_cache"`
	CacheValidTime int         `yaml:"cache_valid_time"`
	Purge          bool        `yaml:"purge"`
	ForceAptGet    bool        `yaml:"force_apt_get"`
	Autoremove     bool        `yaml:"autoremove"`
	Upgrade        string      `yaml:"upgrade"`
}

type UserParams struct {
	Name             string   `yaml:"name"`
	State            string   `yaml:"state,omitempty"`
	UID              *int     `yaml:"uid,omitempty"`
	Group            string   `yaml:"group,omitempty"`
	Groups           []string `yaml:"groups,omitempty"`
	Append           bool     `yaml:"append"`
	Shell            string   `yaml:"shell,omitempty"`
	Home             string   `yaml:"home,omitempty"`
	CreateHome       *bool    `yaml:"create_home,omitempty"`
	MoveHome         bool     `yaml:"move_home"`
	System           bool     `yaml:"system"`
	Password         string   `yaml:"password,omitempty"`
	PasswordLock     *bool    `yaml:"password_lock,omitempty"`
	UpdatePassword   string   `yaml:"update_password,omitempty"`
	Comment          string   `yaml:"comment,omitempty"`
	Expires          *float64 `yaml:"expires,omitempty"`
	Remove           bool     `yaml:"remove"`
	Force            bool     `yaml:"force"`
	Skeleton         string   `yaml:"skeleton,omitempty"`
	NonUnique        bool     `yaml:"non_unique"`
	GenerateSSHKey   bool     `yaml:"generate_ssh_key"`
	SSHKeyBits       int      `yaml:"ssh_key_bits,omitempty"`
	SSHKeyType       string   `yaml:"ssh_key_type,omitempty"`
	SSHKeyFile       string   `yaml:"ssh_key_file,omitempty"`
	SSHKeyComment    string   `yaml:"ssh_key_comment,omitempty"`
	SSHKeyPassphrase string   `yaml:"ssh_key_passphrase,omitempty"`
}

type GroupParams struct {
	Name      string `yaml:"name"`
	State     string `yaml:"state,omitempty"`
	GID       *int   `yaml:"gid,omitempty"`
	System    bool   `yaml:"system"`
	Local     bool   `yaml:"local"`
	NonUnique bool   `yaml:"non_unique"`
	Force     bool   `yaml:"force"`
}

type SystemdServiceParams struct {
	Name         string `yaml:"name,omitempty"`
	State        string `yaml:"state,omitempty"`
	Enabled      *bool  `yaml:"enabled,omitempty"`
	Masked       *bool  `yaml:"masked,omitempty"`
	DaemonReload bool   `yaml:"daemon_reload"`
	DaemonReexec bool   `yaml:"daemon_reexec"`
	Scope        string `yaml:"scope,omitempty"`
	NoBlock      bool   `yaml:"no_block"`
	Force        bool   `yaml:"force"`
}

type ServiceParams struct {
	Name      string `yaml:"name"`
	State     string `yaml:"state,omitempty"`
	Enabled   *bool  `yaml:"enabled,omitempty"`
	Arguments string `yaml:"arguments,omitempty"`
	Pattern   string `yaml:"pattern,omitempty"`
	Sleep     int    `yaml:"sleep,omitempty"`
	Use       string `yaml:"use,omitempty"`
}

type GitParams struct {
	Repo             string `yaml:"repo"`
	Dest             string `yaml:"dest,omitempty"`
	Version          string `yaml:"version,omitempty"`
	Remote           string `yaml:"remote,omitempty"`
	Clone            *bool  `yaml:"clone,omitempty"`
	Update           *bool  `yaml:"update,omitempty"`
	Force            bool   `yaml:"force"`
	Depth            *int   `yaml:"depth,omitempty"`
	Bare             bool   `yaml:"bare"`
	Recursive        *bool  `yaml:"recursive,omitempty"`
	TrackSubmodules  bool   `yaml:"track_submodules"`
	SingleBranch     bool   `yaml:"single_branch"`
	AcceptHostkey    bool   `yaml:"accept_hostkey"`
	AcceptNewhostkey bool   `yaml:"accept_newhostkey"`
	KeyFile          string `yaml:"key_file,omitempty"`
	SSHOpts          string `yaml:"ssh_opts,omitempty"`
	Refspec          string `yaml:"refspec,omitempty"`
	Executable       string `yaml:"executable,omitempty"`
	SeparateGitDir   string `yaml:"separate_git_dir,omitempty"`
}

type LineinfileParams struct {
	Path         string `yaml:"path"`
	Line         string `yaml:"line,omitempty"`
	Regexp       string `yaml:"regexp,omitempty"`
	SearchString string `yaml:"search_string,omitempty"`
	State        string `yaml:"state,omitempty"`
	Backrefs     bool   `yaml:"backrefs"`
	InsertAfter  string `yaml:"insertafter,omitempty"`
	InsertBefore string `yaml:"insertbefore,omitempty"`
	FirstMatch   bool   `yaml:"firstmatch"`
	Create       bool   `yaml:"create"`
	Backup       bool   `yaml:"backup"`
	Mode         string `yaml:"mode,omitempty"`
	Owner        string `yaml:"owner,omitempty"`
	Group        string `yaml:"group,omitempty"`
	Validate     string `yaml:"validate,omitempty"`
}

type BlockinfileParams struct {
	Path           string `yaml:"path"`
	Block          string `yaml:"block,omitempty"`
	Marker         string `yaml:"marker,omitempty"`
	MarkerBegin    string `yaml:"marker_begin,omitempty"`
	MarkerEnd      string `yaml:"marker_end,omitempty"`
	InsertAfter    string `yaml:"insertafter,omitempty"`
	InsertBefore   string `yaml:"insertbefore,omitempty"`
	State          string `yaml:"state,omitempty"`
	Create         bool   `yaml:"create"`
	Backup         bool   `yaml:"backup"`
	Mode           string `yaml:"mode,omitempty"`
	Owner          string `yaml:"owner,omitempty"`
	Group          string `yaml:"group,omitempty"`
	Validate       string `yaml:"validate,omitempty"`
	PrependNewline bool   `yaml:"prepend_newline"`
	AppendNewline  bool   `yaml:"append_newline"`
}

type ReplaceParams struct {
	Path     string `yaml:"path"`
	Regexp   string `yaml:"regexp"`
	Replace  string `yaml:"replace,omitempty"`
	After    string `yaml:"after,omitempty"`
	Before   string `yaml:"before,omitempty"`
	Backup   bool   `yaml:"backup"`
	Mode     string `yaml:"mode,omitempty"`
	Owner    string `yaml:"owner,omitempty"`
	Group    string `yaml:"group,omitempty"`
	Validate string `yaml:"validate,omitempty"`
}

type TcpFlagsParams struct {
	Flags    []string `yaml:"flags,omitempty"`
	FlagsSet []string `yaml:"flags_set,omitempty"`
}

type IptablesStateParams struct {
	Path      string `yaml:"path"`
	State     string `yaml:"state"`
	Table     string `yaml:"table,omitempty"`
	Counters  bool   `yaml:"counters,omitempty"`
	Noflush   bool   `yaml:"noflush,omitempty"`
	IPVersion string `yaml:"ip_version,omitempty"`
	Wait      int    `yaml:"wait,omitempty"`
	Modprobe  string `yaml:"modprobe,omitempty"`
}

type IptablesParams struct {
	Table            string          `yaml:"table,omitempty"`
	Chain            string          `yaml:"chain,omitempty"`
	State            string          `yaml:"state,omitempty"`
	Action           string          `yaml:"action,omitempty"`
	RuleNum          int             `yaml:"rule_num,omitempty"`
	Protocol         string          `yaml:"protocol,omitempty"`
	Source           string          `yaml:"source,omitempty"`
	Destination      string          `yaml:"destination,omitempty"`
	Match            []string        `yaml:"match,omitempty"`
	Jump             string          `yaml:"jump,omitempty"`
	Goto             string          `yaml:"goto,omitempty"`
	InInterface      string          `yaml:"in_interface,omitempty"`
	OutInterface     string          `yaml:"out_interface,omitempty"`
	SourcePort       string          `yaml:"source_port,omitempty"`
	DestinationPort  string          `yaml:"destination_port,omitempty"`
	DestinationPorts []string        `yaml:"destination_ports,omitempty"`
	Ctstate          []string        `yaml:"ctstate,omitempty"`
	Comment          string          `yaml:"comment,omitempty"`
	IcmpType         string          `yaml:"icmp_type,omitempty"`
	Fragment         string          `yaml:"fragment,omitempty"`
	TcpFlags         *TcpFlagsParams `yaml:"tcp_flags,omitempty"`
	Syn              string          `yaml:"syn,omitempty"`
	Limit            string          `yaml:"limit,omitempty"`
	LimitBurst       string          `yaml:"limit_burst,omitempty"`
	LogPrefix        string          `yaml:"log_prefix,omitempty"`
	LogLevel         string          `yaml:"log_level,omitempty"`
	RejectWith       string          `yaml:"reject_with,omitempty"`
	ToDestination    string          `yaml:"to_destination,omitempty"`
	ToSource         string          `yaml:"to_source,omitempty"`
	ToPorts          string          `yaml:"to_ports,omitempty"`
	Gateway          string          `yaml:"gateway,omitempty"`
	SrcRange         string          `yaml:"src_range,omitempty"`
	DstRange         string          `yaml:"dst_range,omitempty"`
	SetCounters      string          `yaml:"set_counters,omitempty"`
	SetDscpMark      string          `yaml:"set_dscp_mark,omitempty"`
	SetDscpMarkClass string          `yaml:"set_dscp_mark_class,omitempty"`
	UidOwner         string          `yaml:"uid_owner,omitempty"`
	GidOwner         string          `yaml:"gid_owner,omitempty"`
	MatchSet         string          `yaml:"match_set,omitempty"`
	MatchSetFlags    string          `yaml:"match_set_flags,omitempty"`
	Flush            bool            `yaml:"flush,omitempty"`
	Policy           string          `yaml:"policy,omitempty"`
	ChainManagement  bool            `yaml:"chain_management,omitempty"`
	IPVersion        string          `yaml:"ip_version,omitempty"`
	Wait             int             `yaml:"wait,omitempty"`
	Numeric          bool            `yaml:"numeric,omitempty"`
}

type DockerContainerParams struct {
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image,omitempty"`
	State          string            `yaml:"state,omitempty"`
	Command        interface{}       `yaml:"command,omitempty"`
	Entrypoint     interface{}       `yaml:"entrypoint,omitempty"`
	Args           []string          `yaml:"args,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	ExposedPorts   []string          `yaml:"exposed_ports,omitempty"`
	Ports          []string          `yaml:"ports,omitempty"`
	Volumes        []string          `yaml:"volumes,omitempty"`
	NetworkMode    string            `yaml:"network_mode,omitempty"`
	Networks       []DockerNetwork   `yaml:"networks,omitempty"`
	NetworksAppend bool              `yaml:"networks_append,omitempty"`
	RestartPolicy  string            `yaml:"restart_policy,omitempty"`
	AutoRemove     bool              `yaml:"auto_remove,omitempty"`
	Privileged     bool              `yaml:"privileged,omitempty"`
	User           string            `yaml:"user,omitempty"`
	WorkingDir     string            `yaml:"working_dir,omitempty"`
	Hostname       string            `yaml:"hostname,omitempty"`
	Domainname     string            `yaml:"domainname,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
	Links          []string          `yaml:"links,omitempty"`
	LogDriver      string            `yaml:"log_driver,omitempty"`
	LogOptions     map[string]string `yaml:"log_options,omitempty"`

	// Capabilities (Tier 1)
	CapAdd  []string `yaml:"cap_add,omitempty"`
	CapDrop []string `yaml:"cap_drop,omitempty"`

	// Devices (Tier 1)
	Devices []string `yaml:"devices,omitempty"`

	// Healthcheck (Tier 1)
	Healthcheck *DockerHealthcheck `yaml:"healthcheck,omitempty"`

	// Other Tier 1 options
	Init    bool     `yaml:"init,omitempty"`
	Tmpfs   []string `yaml:"tmpfs,omitempty"`
	ShmSize string   `yaml:"shm_size,omitempty"`

	// Resource limits (Tier 2)
	Ulimits     []DockerUlimit    `yaml:"ulimits,omitempty"`
	Sysctls     map[string]string `yaml:"sysctls,omitempty"`
	SecurityOpt []string          `yaml:"security_opt,omitempty"`
	CPUs        float64           `yaml:"cpus,omitempty"`
	Memory      string            `yaml:"memory,omitempty"`
	MemorySwap  string            `yaml:"memory_swap,omitempty"`
	PidsLimit   int64             `yaml:"pids_limit,omitempty"`

	// Idempotency control
	Comparisons map[string]string `yaml:"comparisons,omitempty"`
	Recreate    interface{}       `yaml:"recreate,omitempty"` // bool or string (auto/always/never)
	ForceKill   bool              `yaml:"force_kill,omitempty"`
	KeepVolumes bool              `yaml:"keep_volumes,omitempty"`

	// Image pull behavior
	Pull interface{} `yaml:"pull,omitempty"` // bool or string (missing/always/never)

	// Registry authentication
	RegistryUsername string `yaml:"registry_username,omitempty"`
	RegistryPassword string `yaml:"registry_password,omitempty"`

	// Common options
	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerHealthcheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
}

type DockerUlimit struct {
	Name string `yaml:"name"`
	Soft int64  `yaml:"soft"`
	Hard int64  `yaml:"hard"`
}

type DockerNetwork struct {
	Name        string   `yaml:"name"`
	IPv4Address string   `yaml:"ipv4_address,omitempty"`
	IPv6Address string   `yaml:"ipv6_address,omitempty"`
	Links       []string `yaml:"links,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty"`
}

type DockerImageParams struct {
	Name        string `yaml:"name"`
	Tag         string `yaml:"tag,omitempty"`
	Repository  string `yaml:"repository,omitempty"`
	State       string `yaml:"state,omitempty"`
	Source      string `yaml:"source,omitempty"`
	Push        bool   `yaml:"push,omitempty"`
	ArchivePath string `yaml:"archive_path,omitempty"`
	DockerFile  string `yaml:"dockerfile,omitempty"`
	BuildPath   string `yaml:"build_path,omitempty"`
	KeepImage   bool   `yaml:"keep_image,omitempty"`

	// Pull behavior
	Pull interface{} `yaml:"pull,omitempty"` // string (missing/always/never) or bool for backward compat

	// Force flags (separated for clarity)
	ForcePull   bool `yaml:"force_pull,omitempty"`   // Force pull even if image exists
	ForceRemove bool `yaml:"force_remove,omitempty"` // Force remove (removes containers using the image)
	ForceTag    bool `yaml:"force_tag,omitempty"`    // Force tag even if target exists
	ForceSource bool `yaml:"force_source,omitempty"` // Deprecated: use force_pull or force_remove

	// Registry authentication
	RegistryUsername string `yaml:"registry_username,omitempty"`
	RegistryPassword string `yaml:"registry_password,omitempty"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerNetworkParams struct {
	Name       string            `yaml:"name"`
	State      string            `yaml:"state,omitempty"`
	Driver     string            `yaml:"driver,omitempty"`
	Options    map[string]string `yaml:"options,omitempty"`
	IPAMConfig []IPAMConfig      `yaml:"ipam_config,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
	Internal   bool              `yaml:"internal,omitempty"`
	Attachable bool              `yaml:"attachable,omitempty"`
	Scope      string            `yaml:"scope,omitempty"`
	Force      bool              `yaml:"force,omitempty"`

	// Phase 4.1: Connected Containers Management
	Connected []NetworkConnectedContainer `yaml:"connected,omitempty"`
	Appends   bool                        `yaml:"appends,omitempty"`

	// Phase 4.2: Additional Options
	EnableIPv6        bool              `yaml:"enable_ipv6,omitempty"`
	ConfigOnly        bool              `yaml:"config_only,omitempty"`
	ConfigFrom        string            `yaml:"config_from,omitempty"`
	Ingress           bool              `yaml:"ingress,omitempty"`
	IPAMDriver        string            `yaml:"ipam_driver,omitempty"`
	IPAMDriverOptions map[string]string `yaml:"ipam_driver_options,omitempty"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type NetworkConnectedContainer struct {
	Name        string            `yaml:"name"`
	IPv4Address string            `yaml:"ipv4_address,omitempty"`
	IPv6Address string            `yaml:"ipv6_address,omitempty"`
	Aliases     []string          `yaml:"aliases,omitempty"`
	Links       []string          `yaml:"links,omitempty"`
	DriverOpts  map[string]string `yaml:"driver_opts,omitempty"`
}

type IPAMConfig struct {
	Subnet     string            `yaml:"subnet,omitempty"`
	Gateway    string            `yaml:"gateway,omitempty"`
	IPRange    string            `yaml:"ip_range,omitempty"`
	AuxAddress map[string]string `yaml:"aux_address,omitempty"`
}

type DockerVolumeParams struct {
	Name          string            `yaml:"name"`
	State         string            `yaml:"state,omitempty"`
	Driver        string            `yaml:"driver,omitempty"`
	DriverOptions map[string]string `yaml:"driver_options,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Recreate      string            `yaml:"recreate,omitempty"`
	Force         bool              `yaml:"force,omitempty"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerPruneParams struct {
	Containers    bool              `yaml:"containers,omitempty"`
	Images        bool              `yaml:"images,omitempty"`
	Networks      bool              `yaml:"networks,omitempty"`
	Volumes       bool              `yaml:"volumes,omitempty"`
	Builder       bool              `yaml:"builder,omitempty"`
	ImagesFilters map[string]string `yaml:"images_filters,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerLoginParams struct {
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	Registry   string `yaml:"registry,omitempty"`
	Email      string `yaml:"email,omitempty"`
	ConfigPath string `yaml:"config_path,omitempty"`
	State      string `yaml:"state,omitempty"`
	Relogin    bool   `yaml:"relogin,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerSwarmParams struct {
	State           string   `yaml:"state,omitempty"`
	AdvertiseAddr   string   `yaml:"advertise_addr,omitempty"`
	ListenAddr      string   `yaml:"listen_addr,omitempty"`
	ForceNewCluster bool     `yaml:"force_new_cluster,omitempty"`
	RemoteAddrs     []string `yaml:"remote_addrs,omitempty"`
	JoinToken       string   `yaml:"join_token,omitempty"`
	NodeID          string   `yaml:"node_id,omitempty"`
	Force           bool     `yaml:"force,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerComposeParams struct {
	ProjectSrc    string            `yaml:"project_src"`
	ProjectName   string            `yaml:"project_name,omitempty"`
	Files         []string          `yaml:"files,omitempty"`
	State         string            `yaml:"state,omitempty"`
	Services      []string          `yaml:"services,omitempty"`
	Scale         map[string]int    `yaml:"scale,omitempty"`
	Build         bool              `yaml:"build,omitempty"`
	Pull          bool              `yaml:"pull,omitempty"`
	RemoveOrphans bool              `yaml:"remove_orphans,omitempty"`
	Env           map[string]string `yaml:"env,omitempty"`
	Profiles      []string          `yaml:"profiles,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerComposeV2RunParams struct {
	ProjectSrc      string            `yaml:"project_src"`
	ProjectName     string            `yaml:"project_name,omitempty"`
	Files           []string          `yaml:"files,omitempty"`
	Service         string            `yaml:"service"`
	Argv            []string          `yaml:"argv,omitempty"`
	Command         string            `yaml:"command,omitempty"`
	Build           bool              `yaml:"build,omitempty"`
	CapAdd          []string          `yaml:"cap_add,omitempty"`
	CapDrop         []string          `yaml:"cap_drop,omitempty"`
	EntryPoint      string            `yaml:"entrypoint,omitempty"`
	Interactive     *bool             `yaml:"interactive,omitempty"`
	Labels          []string          `yaml:"labels,omitempty"`
	Name            string            `yaml:"name,omitempty"`
	NoDeps          bool              `yaml:"no_deps,omitempty"`
	Publish         []string          `yaml:"publish,omitempty"`
	QuietPull       bool              `yaml:"quiet_pull,omitempty"`
	RemoveOrphans   bool              `yaml:"remove_orphans,omitempty"`
	Cleanup         bool              `yaml:"cleanup,omitempty"`
	ServicePorts    bool              `yaml:"service_ports,omitempty"`
	UseAliases      bool              `yaml:"use_aliases,omitempty"`
	Volumes         []string          `yaml:"volumes,omitempty"`
	Chdir           string            `yaml:"chdir,omitempty"`
	Detach          bool              `yaml:"detach,omitempty"`
	User            string            `yaml:"user,omitempty"`
	Stdin           string            `yaml:"stdin,omitempty"`
	StdinAddNewline *bool             `yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool             `yaml:"strip_empty_ends,omitempty"`
	TTY             *bool             `yaml:"tty,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	Profiles        []string          `yaml:"profiles,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerSecretParams struct {
	Name      string            `yaml:"name"`
	Data      string            `yaml:"data,omitempty"`
	DataIsB64 bool              `yaml:"data_is_b64,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
	Force     bool              `yaml:"force,omitempty"`
	State     string            `yaml:"state,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerConfigParams struct {
	Name      string            `yaml:"name"`
	Data      string            `yaml:"data,omitempty"`
	DataIsB64 bool              `yaml:"data_is_b64,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty"`
	Force     bool              `yaml:"force,omitempty"`
	State     string            `yaml:"state,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerStackParams struct {
	Name             string `yaml:"name"`
	ComposeFile      string `yaml:"compose_file,omitempty"`
	State            string `yaml:"state,omitempty"`
	WithRegistryAuth bool   `yaml:"with_registry_auth,omitempty"`
	Prune            bool   `yaml:"prune,omitempty"`
	ResolveImage     string `yaml:"resolve_image,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerContainerExecParams struct {
	Container       string            `yaml:"container"`
	Argv            []string          `yaml:"argv,omitempty"`
	Command         string            `yaml:"command,omitempty"`
	Chdir           string            `yaml:"chdir,omitempty"`
	Detach          bool              `yaml:"detach,omitempty"`
	User            string            `yaml:"user,omitempty"`
	Stdin           string            `yaml:"stdin,omitempty"`
	StdinAddNewline bool              `yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  bool              `yaml:"strip_empty_ends,omitempty"`
	TTY             bool              `yaml:"tty,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerContainerCopyIntoParams struct {
	Container     string `yaml:"container"`
	Path          string `yaml:"path,omitempty"`
	Content       string `yaml:"content,omitempty"`
	ContentIsB64  bool   `yaml:"content_is_b64,omitempty"`
	ContainerPath string `yaml:"container_path"`
	Follow        bool   `yaml:"follow,omitempty"`
	LocalFollow   bool   `yaml:"local_follow,omitempty"`
	OwnerID       *int   `yaml:"owner_id,omitempty"`
	GroupID       *int   `yaml:"group_id,omitempty"`
	Mode          string `yaml:"mode,omitempty"`
	Force         *bool  `yaml:"force,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerImageBuildParams struct {
	Name       string            `yaml:"name"`
	Tag        string            `yaml:"tag,omitempty"`
	Path       string            `yaml:"path"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	CacheFrom  []string          `yaml:"cache_from,omitempty"`
	Pull       bool              `yaml:"pull,omitempty"`
	Network    string            `yaml:"network,omitempty"`
	NoCache    bool              `yaml:"nocache,omitempty"`
	EtcHosts   map[string]string `yaml:"etc_hosts,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
	Target     string            `yaml:"target,omitempty"`
	Platform   []string          `yaml:"platform,omitempty"`
	ShmSize    string            `yaml:"shm_size,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
	Rebuild    string            `yaml:"rebuild,omitempty"`
	Push       bool              `yaml:"push,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerImageLoadParams struct {
	Path string `yaml:"path"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

type DockerImageExportParams struct {
	Names []string `yaml:"names,omitempty"`
	Name  string   `yaml:"name,omitempty"` // Alias
	Tag   string   `yaml:"tag,omitempty"`
	Path  string   `yaml:"path"`
	Force bool     `yaml:"force,omitempty"`

	DockerHost string `yaml:"docker_host,omitempty"`
	TLS        bool   `yaml:"tls,omitempty"`
}

func (a *AptParams) GetPackages() []string {
	if a.Name == nil {
		return nil
	}
	switch v := a.Name.(type) {
	case string:
		return []string{v}
	case []interface{}:
		pkgs := make([]string, 0, len(v))
		for _, p := range v {
			if s, ok := p.(string); ok {
				pkgs = append(pkgs, s)
			}
		}
		return pkgs
	default:
		return nil
	}
}

// knownYAMLKeys returns the set of valid YAML keys for a struct type.
func knownYAMLKeys(t reflect.Type) map[string]bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	keys := make(map[string]bool)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name != "" {
			keys[name] = true
		}
	}
	return keys
}

// checkUnknownFields validates that all keys in a YAML mapping node are known
// fields for the given struct type. Returns an error listing unknown fields.
func checkUnknownFields(node *yaml.Node, t reflect.Type) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	known := knownYAMLKeys(t)
	var unknown []string
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !known[key] {
			unknown = append(unknown, fmt.Sprintf("%q (line %d)", key, node.Content[i].Line))
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown fields: %s", strings.Join(unknown, ", "))
	}
	return nil
}

func (t *Task) UnmarshalYAML(node *yaml.Node) error {
	// Check for unknown fields before decoding
	if err := checkUnknownFields(node, reflect.TypeOf(Task{})); err != nil {
		return fmt.Errorf("task: %w", err)
	}

	// Unmarshal into an alias type to get normal behavior
	type TaskAlias Task
	var alias TaskAlias
	if err := node.Decode(&alias); err != nil {
		return err
	}
	*t = Task(alias)

	// Now check for keys that exist but have nil values
	// These are modules that can be called with no arguments
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value := node.Content[i+1]

			// If the key exists and the value is null, initialize empty struct
			if value.Kind == yaml.ScalarNode && value.Tag == "!!null" {
				switch key {
				case "ping":
					if t.Ping == nil {
						t.Ping = &PingParams{}
					}
				case "service_facts":
					if t.ServiceFacts == nil {
						t.ServiceFacts = &ServiceFactsParams{}
					}
				case "tempfile":
					if t.Tempfile == nil {
						t.Tempfile = &TempfileParams{}
					}
				case "reboot":
					if t.Reboot == nil {
						t.Reboot = &RebootParams{}
					}
				}
			}

			// Handle import_tasks with scalar string value (free-form syntax)
			if key == "import_tasks" && value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
				t.ImportTasks = &ImportTasksParams{File: value.Value}
			}

			// Handle include_tasks with scalar string value (free-form syntax)
			if key == "include_tasks" && value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
				t.IncludeTasks = &IncludeTasksParams{File: value.Value}
			}
		}
	}

	return nil
}

// Docker Info Modules (Phase 5)

type DockerContainerInfoParams struct {
	Name string `yaml:"name"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerImageInfoParams struct {
	Name string `yaml:"name"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerNetworkInfoParams struct {
	Name string `yaml:"name"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerVolumeInfoParams struct {
	Name string `yaml:"name"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerHostInfoParams struct {
	Containers bool `yaml:"containers,omitempty"`
	Images     bool `yaml:"images,omitempty"`
	Volumes    bool `yaml:"volumes,omitempty"`
	DiskUsage  bool `yaml:"disk_usage,omitempty"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

type DockerSwarmInfoParams struct {
	Nodes   bool `yaml:"nodes,omitempty"`
	Verbose bool `yaml:"verbose,omitempty"`

	DockerHost    string `yaml:"docker_host,omitempty"`
	TLS           bool   `yaml:"tls,omitempty"`
	ValidateCerts bool   `yaml:"validate_certs,omitempty"`
	CAPath        string `yaml:"ca_path,omitempty"`
	ClientCert    string `yaml:"client_cert,omitempty"`
	ClientKey     string `yaml:"client_key,omitempty"`
	APIVersion    string `yaml:"api_version,omitempty"`
	Timeout       int    `yaml:"timeout,omitempty"`
	Debug         bool   `yaml:"debug,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if cfg.VarsMerge == "" {
		cfg.VarsMerge = "replace"
	}

	if cfg.VarsMerge != "replace" && cfg.VarsMerge != "merge" {
		return nil, fmt.Errorf("invalid vars_merge %q (expected replace or merge)", cfg.VarsMerge)
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

	return &cfg, nil
}
