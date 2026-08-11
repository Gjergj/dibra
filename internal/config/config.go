package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gjergjiramku/dibra/internal/modules/registry"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Hosts     []Host                 `json:"hosts" yaml:"hosts"`
	Tasks     []Task                 `json:"tasks" yaml:"tasks"`
	Vars      map[string]interface{} `json:"vars,omitempty" yaml:"vars,omitempty"`
	VarsFiles []string               `json:"vars_files,omitempty" yaml:"vars_files,omitempty"`
	VarsMerge string                 `json:"vars_merge,omitempty" yaml:"vars_merge,omitempty"`
	Inventory string                 `json:"inventory,omitempty" yaml:"inventory,omitempty"`
}

type Host struct {
	Name           string   `json:"name" yaml:"name"`
	Host           string   `json:"host" yaml:"host"`
	Port           int      `json:"port" yaml:"port"`
	User           string   `json:"user" yaml:"user"`
	Password       string   `json:"password" yaml:"password"`
	SSHKeyPath     string   `json:"ssh_key_path" yaml:"ssh_key_path"`
	Become         bool     `json:"become" yaml:"become"`
	BecomePassword string   `json:"become_password" yaml:"become_password"`
	Groups         []string `json:"groups,omitempty" yaml:"groups,omitempty"`
}

type ImportTasksParams struct {
	File string `json:"file,omitempty" yaml:"file,omitempty"`
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
	File string `json:"file,omitempty" yaml:"file,omitempty"`
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

type When []interface{}

func (w *When) UnmarshalYAML(node *yaml.Node) error {
	var raw interface{}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	normalized, err := normalizeWhenValue(raw)
	if err != nil {
		return err
	}
	*w = normalized
	return nil
}

func (w *When) UnmarshalJSON(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	normalized, err := normalizeWhenValue(raw)
	if err != nil {
		return err
	}
	*w = normalized
	return nil
}

func normalizeWhenValue(raw interface{}) (When, error) {
	if raw == nil {
		return nil, nil
	}

	switch v := raw.(type) {
	case When:
		return v, nil
	case string, bool, int, int32, int64, float32, float64, uint, uint32, uint64:
		return When{v}, nil
	case []string:
		normalized := make(When, len(v))
		for i, item := range v {
			normalized[i] = item
		}
		return normalized, nil
	case []interface{}:
		normalized := make(When, len(v))
		for i, item := range v {
			switch item.(type) {
			case string, bool, int, int32, int64, float32, float64, uint, uint32, uint64:
				normalized[i] = item
			default:
				return nil, fmt.Errorf("when list entries must be strings, booleans, or numbers")
			}
		}
		return normalized, nil
	default:
		return nil, fmt.Errorf("when must be a string, boolean, number, or list")
	}
}

type Task struct {
	Name           string                 `json:"name" yaml:"name"`
	Vars           map[string]interface{} `json:"vars,omitempty" yaml:"vars,omitempty"`
	When           When                   `json:"when,omitempty" yaml:"when,omitempty"`
	Loop           interface{}            `json:"loop,omitempty" yaml:"loop,omitempty"`
	WithItems      interface{}            `json:"with_items,omitempty" yaml:"with_items,omitempty"`
	WithList       interface{}            `json:"with_list,omitempty" yaml:"with_list,omitempty"`
	WithDict       interface{}            `json:"with_dict,omitempty" yaml:"with_dict,omitempty"`
	WithSequence   interface{}            `json:"with_sequence,omitempty" yaml:"with_sequence,omitempty"`
	LoopControl    *LoopControlParams     `json:"loop_control,omitempty" yaml:"loop_control,omitempty"`
	ImportTasks    *ImportTasksParams     `json:"import_tasks,omitempty" yaml:"import_tasks,omitempty"`
	IncludeTasks   *IncludeTasksParams    `json:"include_tasks,omitempty" yaml:"include_tasks,omitempty"`
	SourceDir      string                 `yaml:"-"`
	Register       string                 `json:"register,omitempty" yaml:"register,omitempty"`
	CheckMode      *bool                  `json:"check_mode,omitempty" yaml:"check_mode,omitempty"`
	Diff           *bool                  `json:"diff,omitempty" yaml:"diff,omitempty"`
	Template       *TemplateParams        `json:"template,omitempty" yaml:"template,omitempty"`
	Apt            *AptParams             `json:"apt,omitempty" yaml:"apt,omitempty"`
	AptKey         *AptKeyParams          `json:"apt_key,omitempty" yaml:"apt_key,omitempty"`
	AptRepository  *AptRepositoryParams   `json:"apt_repository,omitempty" yaml:"apt_repository,omitempty"`
	File           *FileParams            `json:"file,omitempty" yaml:"file,omitempty"`
	Copy           *CopyParams            `json:"copy,omitempty" yaml:"copy,omitempty"`
	Fetch          *FetchParams           `json:"fetch,omitempty" yaml:"fetch,omitempty"`
	URI            *URIParams             `json:"uri,omitempty" yaml:"uri,omitempty"`
	Cron           *CronParams            `json:"cron,omitempty" yaml:"cron,omitempty"`
	UFW            *UFWParams             `json:"ufw,omitempty" yaml:"ufw,omitempty"`
	User           *UserParams            `json:"user,omitempty" yaml:"user,omitempty"`
	Group          *GroupParams           `json:"group,omitempty" yaml:"group,omitempty"`
	SystemdService *SystemdServiceParams  `json:"systemd_service,omitempty" yaml:"systemd_service,omitempty"`
	Systemd        *SystemdServiceParams  `json:"systemd,omitempty" yaml:"systemd,omitempty"`
	Service        *ServiceParams         `json:"service,omitempty" yaml:"service,omitempty"`
	ServiceFacts   *ServiceFactsParams    `json:"service_facts,omitempty" yaml:"service_facts,omitempty"`
	GatherFacts    *GatherFactsParams     `json:"gather_facts,omitempty" yaml:"gather_facts,omitempty"`
	Ping           *PingParams            `json:"ping,omitempty" yaml:"ping,omitempty"`
	Slurp          *SlurpParams           `json:"slurp,omitempty" yaml:"slurp,omitempty"`
	Command        *CommandParams         `json:"command,omitempty" yaml:"command,omitempty"`
	Shell          *ShellParams           `json:"shell,omitempty" yaml:"shell,omitempty"`
	Script         *ScriptParams          `json:"script,omitempty" yaml:"script,omitempty"`
	Unarchive      *UnarchiveParams       `json:"unarchive,omitempty" yaml:"unarchive,omitempty"`
	Git            *GitParams             `json:"git,omitempty" yaml:"git,omitempty"`
	Lineinfile     *LineinfileParams      `json:"lineinfile,omitempty" yaml:"lineinfile,omitempty"`
	Blockinfile    *BlockinfileParams     `json:"blockinfile,omitempty" yaml:"blockinfile,omitempty"`
	Replace        *ReplaceParams         `json:"replace,omitempty" yaml:"replace,omitempty"`
	Iptables       *IptablesParams        `json:"iptables,omitempty" yaml:"iptables,omitempty"`
	IptablesState  *IptablesStateParams   `json:"iptables_state,omitempty" yaml:"iptables_state,omitempty"`
	Tempfile       *TempfileParams        `json:"tempfile,omitempty" yaml:"tempfile,omitempty"`
	Find           *FindParams            `json:"find,omitempty" yaml:"find,omitempty"`
	Reboot         *RebootParams          `json:"reboot,omitempty" yaml:"reboot,omitempty"`
	Module         *registry.Invocation   `json:"-" yaml:"-"`
}

type LoopControlParams struct {
	LoopVar          string  `json:"loop_var,omitempty" yaml:"loop_var,omitempty"`
	IndexVar         string  `json:"index_var,omitempty" yaml:"index_var,omitempty"`
	Pause            float64 `json:"pause,omitempty" yaml:"pause,omitempty"`
	Extended         bool    `json:"extended,omitempty" yaml:"extended,omitempty"`
	ExtendedAllitems *bool   `json:"extended_allitems,omitempty" yaml:"extended_allitems,omitempty"`
	Label            string  `json:"label,omitempty" yaml:"label,omitempty"`
}

type TemplateParams struct {
	Src                 string `json:"src" yaml:"src"`
	Dest                string `json:"dest" yaml:"dest"`
	Mode                string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner               string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group               string `json:"group,omitempty" yaml:"group,omitempty"`
	Backup              bool   `json:"backup" yaml:"backup"`
	Force               *bool  `json:"force,omitempty" yaml:"force,omitempty"`
	Follow              bool   `json:"follow" yaml:"follow"`
	Validate            string `json:"validate,omitempty" yaml:"validate,omitempty"`
	NewlineSequence     string `json:"newline_sequence,omitempty" yaml:"newline_sequence,omitempty"`
	VariableStartString string `json:"variable_start_string,omitempty" yaml:"variable_start_string,omitempty"`
	VariableEndString   string `json:"variable_end_string,omitempty" yaml:"variable_end_string,omitempty"`
	BlockStartString    string `json:"block_start_string,omitempty" yaml:"block_start_string,omitempty"`
	BlockEndString      string `json:"block_end_string,omitempty" yaml:"block_end_string,omitempty"`
	CommentStartString  string `json:"comment_start_string,omitempty" yaml:"comment_start_string,omitempty"`
	CommentEndString    string `json:"comment_end_string,omitempty" yaml:"comment_end_string,omitempty"`
	TrimBlocks          *bool  `json:"trim_blocks,omitempty" yaml:"trim_blocks,omitempty"`
	LstripBlocks        *bool  `json:"lstrip_blocks,omitempty" yaml:"lstrip_blocks,omitempty"`
}

type ServiceFactsParams struct {
}

type GatherFactsParams struct {
	GatherSubset interface{} `json:"gather_subset,omitempty" yaml:"gather_subset,omitempty"`
	Filter       interface{} `json:"filter,omitempty" yaml:"filter,omitempty"`
	FactPath     string      `json:"fact_path,omitempty" yaml:"fact_path,omitempty"`
}

type TempfileParams struct {
	Path   string  `json:"path,omitempty" yaml:"path,omitempty"`
	Prefix *string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Suffix string  `json:"suffix,omitempty" yaml:"suffix,omitempty"`
	State  string  `json:"state,omitempty" yaml:"state,omitempty"`
}

type FindParams struct {
	Paths             []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	Path              []string `json:"path,omitempty" yaml:"path,omitempty"`
	Name              []string `json:"name,omitempty" yaml:"name,omitempty"`
	Patterns          []string `json:"patterns,omitempty" yaml:"patterns,omitempty"`
	Pattern           []string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Excludes          []string `json:"excludes,omitempty" yaml:"excludes,omitempty"`
	Exclude           []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	Contains          string   `json:"contains,omitempty" yaml:"contains,omitempty"`
	ReadWholeFile     bool     `json:"read_whole_file,omitempty" yaml:"read_whole_file,omitempty"`
	FileType          string   `json:"file_type,omitempty" yaml:"file_type,omitempty"`
	Age               string   `json:"age,omitempty" yaml:"age,omitempty"`
	AgeStamp          string   `json:"age_stamp,omitempty" yaml:"age_stamp,omitempty"`
	Size              string   `json:"size,omitempty" yaml:"size,omitempty"`
	Recurse           bool     `json:"recurse,omitempty" yaml:"recurse,omitempty"`
	Hidden            bool     `json:"hidden,omitempty" yaml:"hidden,omitempty"`
	Follow            bool     `json:"follow,omitempty" yaml:"follow,omitempty"`
	GetChecksum       bool     `json:"get_checksum,omitempty" yaml:"get_checksum,omitempty"`
	ChecksumAlgorithm string   `json:"checksum_algorithm,omitempty" yaml:"checksum_algorithm,omitempty"`
	UseRegex          bool     `json:"use_regex,omitempty" yaml:"use_regex,omitempty"`
	Depth             int      `json:"depth,omitempty" yaml:"depth,omitempty"`
	Mode              string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	ExactMode         *bool    `json:"exact_mode,omitempty" yaml:"exact_mode,omitempty"`
	Limit             int      `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type RebootParams struct {
	PreRebootDelay  int      `json:"pre_reboot_delay,omitempty" yaml:"pre_reboot_delay,omitempty"`
	PostRebootDelay int      `json:"post_reboot_delay,omitempty" yaml:"post_reboot_delay,omitempty"`
	RebootTimeout   int      `json:"reboot_timeout,omitempty" yaml:"reboot_timeout,omitempty"`
	ConnectTimeout  int      `json:"connect_timeout,omitempty" yaml:"connect_timeout,omitempty"`
	TestCommand     string   `json:"test_command,omitempty" yaml:"test_command,omitempty"`
	Msg             string   `json:"msg,omitempty" yaml:"msg,omitempty"`
	SearchPaths     []string `json:"search_paths,omitempty" yaml:"search_paths,omitempty"`
	BootTimeCommand string   `json:"boot_time_command,omitempty" yaml:"boot_time_command,omitempty"`
	RebootCommand   string   `json:"reboot_command,omitempty" yaml:"reboot_command,omitempty"`
}

type PingParams struct {
	Data string `json:"data,omitempty" yaml:"data,omitempty"`
}

type SlurpParams struct {
	Src  string `json:"src,omitempty" yaml:"src,omitempty"`
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type CommandParams struct {
	Cmd             string   `json:"cmd,omitempty" yaml:"cmd,omitempty"`
	Argv            []string `json:"argv,omitempty" yaml:"argv,omitempty"`
	Chdir           string   `json:"chdir,omitempty" yaml:"chdir,omitempty"`
	Creates         string   `json:"creates,omitempty" yaml:"creates,omitempty"`
	Removes         string   `json:"removes,omitempty" yaml:"removes,omitempty"`
	Stdin           string   `json:"stdin,omitempty" yaml:"stdin,omitempty"`
	StdinAddNewline *bool    `json:"stdin_add_newline,omitempty" yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool    `json:"strip_empty_ends,omitempty" yaml:"strip_empty_ends,omitempty"`
}

type ShellParams struct {
	Cmd             string `json:"cmd,omitempty" yaml:"cmd,omitempty"`
	Chdir           string `json:"chdir,omitempty" yaml:"chdir,omitempty"`
	Creates         string `json:"creates,omitempty" yaml:"creates,omitempty"`
	Removes         string `json:"removes,omitempty" yaml:"removes,omitempty"`
	Stdin           string `json:"stdin,omitempty" yaml:"stdin,omitempty"`
	StdinAddNewline *bool  `json:"stdin_add_newline,omitempty" yaml:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool  `json:"strip_empty_ends,omitempty" yaml:"strip_empty_ends,omitempty"`
	Executable      string `json:"executable,omitempty" yaml:"executable,omitempty"`
}

type ScriptParams struct {
	Cmd            string `json:"cmd,omitempty" yaml:"cmd,omitempty"`
	Chdir          string `json:"chdir,omitempty" yaml:"chdir,omitempty"`
	Creates        string `json:"creates,omitempty" yaml:"creates,omitempty"`
	Removes        string `json:"removes,omitempty" yaml:"removes,omitempty"`
	Executable     string `json:"executable,omitempty" yaml:"executable,omitempty"`
	StripEmptyEnds *bool  `json:"strip_empty_ends,omitempty" yaml:"strip_empty_ends,omitempty"`
}

type CopyParams struct {
	Src       string `json:"src,omitempty" yaml:"src,omitempty"`
	Dest      string `json:"dest" yaml:"dest"`
	Content   string `json:"content,omitempty" yaml:"content,omitempty"`
	Mode      string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner     string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group     string `json:"group,omitempty" yaml:"group,omitempty"`
	Backup    bool   `json:"backup" yaml:"backup"`
	Force     bool   `json:"force" yaml:"force"`
	RemoteSrc bool   `json:"remote_src" yaml:"remote_src"`
}

type UnarchiveParams struct {
	Src       string   `json:"src" yaml:"src"`
	Dest      string   `json:"dest" yaml:"dest"`
	RemoteSrc bool     `json:"remote_src" yaml:"remote_src"`
	Creates   string   `json:"creates,omitempty" yaml:"creates,omitempty"`
	ListFiles bool     `json:"list_files" yaml:"list_files"`
	Exclude   []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	Include   []string `json:"include,omitempty" yaml:"include,omitempty"`
	KeepNewer bool     `json:"keep_newer" yaml:"keep_newer"`
	ExtraOpts []string `json:"extra_opts,omitempty" yaml:"extra_opts,omitempty"`
	Mode      string   `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner     string   `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group     string   `json:"group,omitempty" yaml:"group,omitempty"`
}

type FetchParams struct {
	Src              string `json:"src" yaml:"src"`
	Dest             string `json:"dest" yaml:"dest"`
	Flat             bool   `json:"flat" yaml:"flat"`
	FailOnMissing    *bool  `json:"fail_on_missing,omitempty" yaml:"fail_on_missing,omitempty"`
	ValidateChecksum *bool  `json:"validate_checksum,omitempty" yaml:"validate_checksum,omitempty"`
}

type URIParams struct {
	URL             string            `json:"url" yaml:"url"`
	Method          string            `json:"method,omitempty" yaml:"method,omitempty"`
	Body            interface{}       `json:"body,omitempty" yaml:"body,omitempty"`
	BodyFormat      string            `json:"body_format,omitempty" yaml:"body_format,omitempty"`
	Headers         map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	StatusCode      []int             `json:"status_code,omitempty" yaml:"status_code,omitempty"`
	Timeout         int               `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	ReturnContent   bool              `json:"return_content" yaml:"return_content"`
	Dest            string            `json:"dest,omitempty" yaml:"dest,omitempty"`
	Creates         string            `json:"creates,omitempty" yaml:"creates,omitempty"`
	URLUsername     string            `json:"url_username,omitempty" yaml:"url_username,omitempty"`
	URLPassword     string            `json:"url_password,omitempty" yaml:"url_password,omitempty"`
	ForceBasicAuth  bool              `json:"force_basic_auth" yaml:"force_basic_auth"`
	FollowRedirects string            `json:"follow_redirects,omitempty" yaml:"follow_redirects,omitempty"`
	ValidateCerts   *bool             `json:"validate_certs,omitempty" yaml:"validate_certs,omitempty"`
}

type CronParams struct {
	Name         string `json:"name" yaml:"name"`
	User         string `json:"user,omitempty" yaml:"user,omitempty"`
	Job          string `json:"job,omitempty" yaml:"job,omitempty"`
	State        string `json:"state,omitempty" yaml:"state,omitempty"`
	Minute       string `json:"minute,omitempty" yaml:"minute,omitempty"`
	Hour         string `json:"hour,omitempty" yaml:"hour,omitempty"`
	Day          string `json:"day,omitempty" yaml:"day,omitempty"`
	Month        string `json:"month,omitempty" yaml:"month,omitempty"`
	Weekday      string `json:"weekday,omitempty" yaml:"weekday,omitempty"`
	SpecialTime  string `json:"special_time,omitempty" yaml:"special_time,omitempty"`
	Disabled     bool   `json:"disabled" yaml:"disabled"`
	Backup       bool   `json:"backup" yaml:"backup"`
	CronFile     string `json:"cron_file,omitempty" yaml:"cron_file,omitempty"`
	Env          bool   `json:"env" yaml:"env"`
	InsertAfter  string `json:"insertafter,omitempty" yaml:"insertafter,omitempty"`
	InsertBefore string `json:"insertbefore,omitempty" yaml:"insertbefore,omitempty"`
}

type UFWParams struct {
	State            string `json:"state,omitempty" yaml:"state,omitempty"`
	Logging          string `json:"logging,omitempty" yaml:"logging,omitempty"`
	Default          string `json:"default,omitempty" yaml:"default,omitempty"`
	Policy           string `json:"policy,omitempty" yaml:"policy,omitempty"`
	Direction        string `json:"direction,omitempty" yaml:"direction,omitempty"`
	Rule             string `json:"rule,omitempty" yaml:"rule,omitempty"`
	Delete           bool   `json:"delete" yaml:"delete"`
	Insert           int    `json:"insert,omitempty" yaml:"insert,omitempty"`
	InsertRelativeTo string `json:"insert_relative_to,omitempty" yaml:"insert_relative_to,omitempty"`
	Interface        string `json:"interface,omitempty" yaml:"interface,omitempty"`
	If               string `json:"if,omitempty" yaml:"if,omitempty"`
	InterfaceIn      string `json:"interface_in,omitempty" yaml:"interface_in,omitempty"`
	IfIn             string `json:"if_in,omitempty" yaml:"if_in,omitempty"`
	InterfaceOut     string `json:"interface_out,omitempty" yaml:"interface_out,omitempty"`
	IfOut            string `json:"if_out,omitempty" yaml:"if_out,omitempty"`
	FromIP           string `json:"from_ip,omitempty" yaml:"from_ip,omitempty"`
	From             string `json:"from,omitempty" yaml:"from,omitempty"`
	Src              string `json:"src,omitempty" yaml:"src,omitempty"`
	FromPort         string `json:"from_port,omitempty" yaml:"from_port,omitempty"`
	ToIP             string `json:"to_ip,omitempty" yaml:"to_ip,omitempty"`
	Dest             string `json:"dest,omitempty" yaml:"dest,omitempty"`
	To               string `json:"to,omitempty" yaml:"to,omitempty"`
	ToPort           string `json:"to_port,omitempty" yaml:"to_port,omitempty"`
	Port             string `json:"port,omitempty" yaml:"port,omitempty"`
	Proto            string `json:"proto,omitempty" yaml:"proto,omitempty"`
	Protocol         string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Name             string `json:"name,omitempty" yaml:"name,omitempty"`
	App              string `json:"app,omitempty" yaml:"app,omitempty"`
	Route            bool   `json:"route" yaml:"route"`
	Log              bool   `json:"log" yaml:"log"`
	Comment          string `json:"comment,omitempty" yaml:"comment,omitempty"`
}

type FileParams struct {
	Path    string `json:"path" yaml:"path"`
	State   string `json:"state,omitempty" yaml:"state,omitempty"`
	Mode    string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner   string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group   string `json:"group,omitempty" yaml:"group,omitempty"`
	Src     string `json:"src,omitempty" yaml:"src,omitempty"`
	Recurse bool   `json:"recurse" yaml:"recurse"`
	Force   bool   `json:"force" yaml:"force"`
	Follow  bool   `json:"follow" yaml:"follow"`
}

type AptKeyParams struct {
	URL     string `json:"url,omitempty" yaml:"url,omitempty"`
	Data    string `json:"data,omitempty" yaml:"data,omitempty"`
	File    string `json:"file,omitempty" yaml:"file,omitempty"`
	Keyring string `json:"keyring,omitempty" yaml:"keyring,omitempty"`
	ID      string `json:"id,omitempty" yaml:"id,omitempty"`
	State   string `json:"state,omitempty" yaml:"state,omitempty"`
}

type AptRepositoryParams struct {
	Repo        string `json:"repo" yaml:"repo"`
	State       string `json:"state,omitempty" yaml:"state,omitempty"`
	Filename    string `json:"filename,omitempty" yaml:"filename,omitempty"`
	UpdateCache bool   `json:"update_cache" yaml:"update_cache"`
}

type AptParams struct {
	Name           interface{} `json:"name" yaml:"name"`
	State          string      `json:"state" yaml:"state"`
	UpdateCache    bool        `json:"update_cache" yaml:"update_cache"`
	CacheValidTime int         `json:"cache_valid_time" yaml:"cache_valid_time"`
	Purge          bool        `json:"purge" yaml:"purge"`
	ForceAptGet    bool        `json:"force_apt_get" yaml:"force_apt_get"`
	Autoremove     bool        `json:"autoremove" yaml:"autoremove"`
	Upgrade        string      `json:"upgrade" yaml:"upgrade"`
}

type UserParams struct {
	Name             string   `json:"name" yaml:"name"`
	State            string   `json:"state,omitempty" yaml:"state,omitempty"`
	UID              *int     `json:"uid,omitempty" yaml:"uid,omitempty"`
	Group            string   `json:"group,omitempty" yaml:"group,omitempty"`
	Groups           []string `json:"groups,omitempty" yaml:"groups,omitempty"`
	Append           bool     `json:"append" yaml:"append"`
	Shell            string   `json:"shell,omitempty" yaml:"shell,omitempty"`
	Home             string   `json:"home,omitempty" yaml:"home,omitempty"`
	CreateHome       *bool    `json:"create_home,omitempty" yaml:"create_home,omitempty"`
	MoveHome         bool     `json:"move_home" yaml:"move_home"`
	System           bool     `json:"system" yaml:"system"`
	Password         string   `json:"password,omitempty" yaml:"password,omitempty"`
	PasswordLock     *bool    `json:"password_lock,omitempty" yaml:"password_lock,omitempty"`
	UpdatePassword   string   `json:"update_password,omitempty" yaml:"update_password,omitempty"`
	Comment          string   `json:"comment,omitempty" yaml:"comment,omitempty"`
	Expires          *float64 `json:"expires,omitempty" yaml:"expires,omitempty"`
	Remove           bool     `json:"remove" yaml:"remove"`
	Force            bool     `json:"force" yaml:"force"`
	Skeleton         string   `json:"skeleton,omitempty" yaml:"skeleton,omitempty"`
	NonUnique        bool     `json:"non_unique" yaml:"non_unique"`
	GenerateSSHKey   bool     `json:"generate_ssh_key" yaml:"generate_ssh_key"`
	SSHKeyBits       int      `json:"ssh_key_bits,omitempty" yaml:"ssh_key_bits,omitempty"`
	SSHKeyType       string   `json:"ssh_key_type,omitempty" yaml:"ssh_key_type,omitempty"`
	SSHKeyFile       string   `json:"ssh_key_file,omitempty" yaml:"ssh_key_file,omitempty"`
	SSHKeyComment    string   `json:"ssh_key_comment,omitempty" yaml:"ssh_key_comment,omitempty"`
	SSHKeyPassphrase string   `json:"ssh_key_passphrase,omitempty" yaml:"ssh_key_passphrase,omitempty"`
}

type GroupParams struct {
	Name      string `json:"name" yaml:"name"`
	State     string `json:"state,omitempty" yaml:"state,omitempty"`
	GID       *int   `json:"gid,omitempty" yaml:"gid,omitempty"`
	System    bool   `json:"system" yaml:"system"`
	Local     bool   `json:"local" yaml:"local"`
	NonUnique bool   `json:"non_unique" yaml:"non_unique"`
	Force     bool   `json:"force" yaml:"force"`
}

type SystemdServiceParams struct {
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	State        string `json:"state,omitempty" yaml:"state,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Masked       *bool  `json:"masked,omitempty" yaml:"masked,omitempty"`
	DaemonReload bool   `json:"daemon_reload" yaml:"daemon_reload"`
	DaemonReexec bool   `json:"daemon_reexec" yaml:"daemon_reexec"`
	Scope        string `json:"scope,omitempty" yaml:"scope,omitempty"`
	NoBlock      bool   `json:"no_block" yaml:"no_block"`
	Force        bool   `json:"force" yaml:"force"`
}

type ServiceParams struct {
	Name      string `json:"name" yaml:"name"`
	State     string `json:"state,omitempty" yaml:"state,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Arguments string `json:"arguments,omitempty" yaml:"arguments,omitempty"`
	Pattern   string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Sleep     int    `json:"sleep,omitempty" yaml:"sleep,omitempty"`
	Use       string `json:"use,omitempty" yaml:"use,omitempty"`
}

type GitParams struct {
	Repo             string `json:"repo" yaml:"repo"`
	Dest             string `json:"dest,omitempty" yaml:"dest,omitempty"`
	Version          string `json:"version,omitempty" yaml:"version,omitempty"`
	Remote           string `json:"remote,omitempty" yaml:"remote,omitempty"`
	Clone            *bool  `json:"clone,omitempty" yaml:"clone,omitempty"`
	Update           *bool  `json:"update,omitempty" yaml:"update,omitempty"`
	Force            bool   `json:"force" yaml:"force"`
	Depth            *int   `json:"depth,omitempty" yaml:"depth,omitempty"`
	Bare             bool   `json:"bare" yaml:"bare"`
	Recursive        *bool  `json:"recursive,omitempty" yaml:"recursive,omitempty"`
	TrackSubmodules  bool   `json:"track_submodules" yaml:"track_submodules"`
	SingleBranch     bool   `json:"single_branch" yaml:"single_branch"`
	AcceptHostkey    bool   `json:"accept_hostkey" yaml:"accept_hostkey"`
	AcceptNewhostkey bool   `json:"accept_newhostkey" yaml:"accept_newhostkey"`
	KeyFile          string `json:"key_file,omitempty" yaml:"key_file,omitempty"`
	SSHOpts          string `json:"ssh_opts,omitempty" yaml:"ssh_opts,omitempty"`
	Refspec          string `json:"refspec,omitempty" yaml:"refspec,omitempty"`
	Executable       string `json:"executable,omitempty" yaml:"executable,omitempty"`
	SeparateGitDir   string `json:"separate_git_dir,omitempty" yaml:"separate_git_dir,omitempty"`
}

type LineinfileParams struct {
	Path         string `json:"path" yaml:"path"`
	Line         string `json:"line,omitempty" yaml:"line,omitempty"`
	Regexp       string `json:"regexp,omitempty" yaml:"regexp,omitempty"`
	SearchString string `json:"search_string,omitempty" yaml:"search_string,omitempty"`
	State        string `json:"state,omitempty" yaml:"state,omitempty"`
	Backrefs     bool   `json:"backrefs" yaml:"backrefs"`
	InsertAfter  string `json:"insertafter,omitempty" yaml:"insertafter,omitempty"`
	InsertBefore string `json:"insertbefore,omitempty" yaml:"insertbefore,omitempty"`
	FirstMatch   bool   `json:"firstmatch" yaml:"firstmatch"`
	Create       bool   `json:"create" yaml:"create"`
	Backup       bool   `json:"backup" yaml:"backup"`
	Mode         string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner        string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group        string `json:"group,omitempty" yaml:"group,omitempty"`
	Validate     string `json:"validate,omitempty" yaml:"validate,omitempty"`
}

type BlockinfileParams struct {
	Path           string `json:"path" yaml:"path"`
	Block          string `json:"block,omitempty" yaml:"block,omitempty"`
	Marker         string `json:"marker,omitempty" yaml:"marker,omitempty"`
	MarkerBegin    string `json:"marker_begin,omitempty" yaml:"marker_begin,omitempty"`
	MarkerEnd      string `json:"marker_end,omitempty" yaml:"marker_end,omitempty"`
	InsertAfter    string `json:"insertafter,omitempty" yaml:"insertafter,omitempty"`
	InsertBefore   string `json:"insertbefore,omitempty" yaml:"insertbefore,omitempty"`
	State          string `json:"state,omitempty" yaml:"state,omitempty"`
	Create         bool   `json:"create" yaml:"create"`
	Backup         bool   `json:"backup" yaml:"backup"`
	Mode           string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner          string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group          string `json:"group,omitempty" yaml:"group,omitempty"`
	Validate       string `json:"validate,omitempty" yaml:"validate,omitempty"`
	PrependNewline bool   `json:"prepend_newline" yaml:"prepend_newline"`
	AppendNewline  bool   `json:"append_newline" yaml:"append_newline"`
}

type ReplaceParams struct {
	Path     string `json:"path" yaml:"path"`
	Regexp   string `json:"regexp" yaml:"regexp"`
	Replace  string `json:"replace,omitempty" yaml:"replace,omitempty"`
	After    string `json:"after,omitempty" yaml:"after,omitempty"`
	Before   string `json:"before,omitempty" yaml:"before,omitempty"`
	Backup   bool   `json:"backup" yaml:"backup"`
	Mode     string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Owner    string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Group    string `json:"group,omitempty" yaml:"group,omitempty"`
	Validate string `json:"validate,omitempty" yaml:"validate,omitempty"`
}

type TcpFlagsParams struct {
	Flags    []string `json:"flags,omitempty" yaml:"flags,omitempty"`
	FlagsSet []string `json:"flags_set,omitempty" yaml:"flags_set,omitempty"`
}

type IptablesStateParams struct {
	Path      string `json:"path" yaml:"path"`
	State     string `json:"state" yaml:"state"`
	Table     string `json:"table,omitempty" yaml:"table,omitempty"`
	Counters  bool   `json:"counters,omitempty" yaml:"counters,omitempty"`
	Noflush   bool   `json:"noflush,omitempty" yaml:"noflush,omitempty"`
	IPVersion string `json:"ip_version,omitempty" yaml:"ip_version,omitempty"`
	Wait      int    `json:"wait,omitempty" yaml:"wait,omitempty"`
	Modprobe  string `json:"modprobe,omitempty" yaml:"modprobe,omitempty"`
}

type IptablesParams struct {
	Table            string          `json:"table,omitempty" yaml:"table,omitempty"`
	Chain            string          `json:"chain,omitempty" yaml:"chain,omitempty"`
	State            string          `json:"state,omitempty" yaml:"state,omitempty"`
	Action           string          `json:"action,omitempty" yaml:"action,omitempty"`
	RuleNum          int             `json:"rule_num,omitempty" yaml:"rule_num,omitempty"`
	Protocol         string          `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Source           string          `json:"source,omitempty" yaml:"source,omitempty"`
	Destination      string          `json:"destination,omitempty" yaml:"destination,omitempty"`
	Match            []string        `json:"match,omitempty" yaml:"match,omitempty"`
	Jump             string          `json:"jump,omitempty" yaml:"jump,omitempty"`
	Goto             string          `json:"goto,omitempty" yaml:"goto,omitempty"`
	InInterface      string          `json:"in_interface,omitempty" yaml:"in_interface,omitempty"`
	OutInterface     string          `json:"out_interface,omitempty" yaml:"out_interface,omitempty"`
	SourcePort       string          `json:"source_port,omitempty" yaml:"source_port,omitempty"`
	DestinationPort  string          `json:"destination_port,omitempty" yaml:"destination_port,omitempty"`
	DestinationPorts []string        `json:"destination_ports,omitempty" yaml:"destination_ports,omitempty"`
	Ctstate          []string        `json:"ctstate,omitempty" yaml:"ctstate,omitempty"`
	Comment          string          `json:"comment,omitempty" yaml:"comment,omitempty"`
	IcmpType         string          `json:"icmp_type,omitempty" yaml:"icmp_type,omitempty"`
	Fragment         string          `json:"fragment,omitempty" yaml:"fragment,omitempty"`
	TcpFlags         *TcpFlagsParams `json:"tcp_flags,omitempty" yaml:"tcp_flags,omitempty"`
	Syn              string          `json:"syn,omitempty" yaml:"syn,omitempty"`
	Limit            string          `json:"limit,omitempty" yaml:"limit,omitempty"`
	LimitBurst       string          `json:"limit_burst,omitempty" yaml:"limit_burst,omitempty"`
	LogPrefix        string          `json:"log_prefix,omitempty" yaml:"log_prefix,omitempty"`
	LogLevel         string          `json:"log_level,omitempty" yaml:"log_level,omitempty"`
	RejectWith       string          `json:"reject_with,omitempty" yaml:"reject_with,omitempty"`
	ToDestination    string          `json:"to_destination,omitempty" yaml:"to_destination,omitempty"`
	ToSource         string          `json:"to_source,omitempty" yaml:"to_source,omitempty"`
	ToPorts          string          `json:"to_ports,omitempty" yaml:"to_ports,omitempty"`
	Gateway          string          `json:"gateway,omitempty" yaml:"gateway,omitempty"`
	SrcRange         string          `json:"src_range,omitempty" yaml:"src_range,omitempty"`
	DstRange         string          `json:"dst_range,omitempty" yaml:"dst_range,omitempty"`
	SetCounters      string          `json:"set_counters,omitempty" yaml:"set_counters,omitempty"`
	SetDscpMark      string          `json:"set_dscp_mark,omitempty" yaml:"set_dscp_mark,omitempty"`
	SetDscpMarkClass string          `json:"set_dscp_mark_class,omitempty" yaml:"set_dscp_mark_class,omitempty"`
	UidOwner         string          `json:"uid_owner,omitempty" yaml:"uid_owner,omitempty"`
	GidOwner         string          `json:"gid_owner,omitempty" yaml:"gid_owner,omitempty"`
	MatchSet         string          `json:"match_set,omitempty" yaml:"match_set,omitempty"`
	MatchSetFlags    string          `json:"match_set_flags,omitempty" yaml:"match_set_flags,omitempty"`
	Flush            bool            `json:"flush,omitempty" yaml:"flush,omitempty"`
	Policy           string          `json:"policy,omitempty" yaml:"policy,omitempty"`
	ChainManagement  bool            `json:"chain_management,omitempty" yaml:"chain_management,omitempty"`
	IPVersion        string          `json:"ip_version,omitempty" yaml:"ip_version,omitempty"`
	Wait             int             `json:"wait,omitempty" yaml:"wait,omitempty"`
	Numeric          bool            `json:"numeric,omitempty" yaml:"numeric,omitempty"`
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
func checkUnknownFields(node *yaml.Node, t reflect.Type, additionalKeys ...string) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	known := knownYAMLKeys(t)
	for _, key := range additionalKeys {
		known[key] = true
	}
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
	if err := checkUnknownFields(node, reflect.TypeOf(Task{}), registry.Names()...); err != nil {
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
		var registeredModuleKey string
		var registeredModuleValue *yaml.Node
		var otherModuleKey string
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i].Value
			value := node.Content[i+1]
			if _, registered := registry.Lookup(key); registered {
				if registeredModuleKey != "" {
					return fmt.Errorf("task: multiple registered modules %q and %q", registeredModuleKey, key)
				}
				registeredModuleKey = key
				registeredModuleValue = value
			} else if isTaskModuleKey(key) {
				otherModuleKey = key
			}

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
				case "gather_facts":
					if t.GatherFacts == nil {
						t.GatherFacts = &GatherFactsParams{}
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

		if registeredModuleKey != "" {
			if otherModuleKey != "" {
				return fmt.Errorf("task: multiple modules %q and %q", registeredModuleKey, otherModuleKey)
			}
			invocation, err := decodeRegisteredModuleYAML(registeredModuleKey, registeredModuleValue)
			if err != nil {
				return fmt.Errorf("task: %w", err)
			}
			t.Module = invocation
		}
	}

	return nil
}

func isTaskModuleKey(key string) bool {
	switch key {
	case "name", "vars", "when", "loop", "with_items", "with_list", "with_dict", "with_sequence", "loop_control", "register", "check_mode", "diff":
		return false
	default:
		return true
	}
}

func decodeRegisteredModuleYAML(name string, node *yaml.Node) (*registry.Invocation, error) {
	var raw any = map[string]any{}
	if node != nil && !(node.Kind == yaml.ScalarNode && node.Tag == "!!null") {
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode %s arguments: %w", name, err)
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode %s arguments: %w", name, err)
	}
	return registry.Decode(name, data)
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
