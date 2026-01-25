package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Hosts []Host `yaml:"hosts"`
	Tasks []Task `yaml:"tasks"`
}

type Host struct {
	Name           string `yaml:"name"`
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	User           string `yaml:"user"`
	Password       string `yaml:"password"`
	SSHKeyPath     string `yaml:"ssh_key_path"`
	Become         bool   `yaml:"become"`
	BecomePassword string `yaml:"become_password"`
}

type Task struct {
	Name           string                 `yaml:"name"`
	Apt            *AptParams             `yaml:"apt,omitempty"`
	AptKey         *AptKeyParams          `yaml:"apt_key,omitempty"`
	AptRepository  *AptRepositoryParams   `yaml:"apt_repository,omitempty"`
	File           *FileParams            `yaml:"file,omitempty"`
	Copy           *CopyParams            `yaml:"copy,omitempty"`
	Fetch          *FetchParams           `yaml:"fetch,omitempty"`
	URI            *URIParams             `yaml:"uri,omitempty"`
	Cron           *CronParams            `yaml:"cron,omitempty"`
	UFW            *UFWParams             `yaml:"ufw,omitempty"`
	User           *UserParams            `yaml:"user,omitempty"`
	SystemdService *SystemdServiceParams  `yaml:"systemd_service,omitempty"`
	Systemd        *SystemdServiceParams  `yaml:"systemd,omitempty"`
	Service        *ServiceParams         `yaml:"service,omitempty"`
	ServiceFacts   *ServiceFactsParams    `yaml:"service_facts,omitempty"`
}

type ServiceFactsParams struct {
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

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
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
