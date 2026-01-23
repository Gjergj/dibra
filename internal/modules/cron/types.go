package cron

type Request struct {
	Name         string `json:"name"`
	User         string `json:"user,omitempty"`
	Job          string `json:"job,omitempty"`
	State        string `json:"state,omitempty"`
	Minute       string `json:"minute,omitempty"`
	Hour         string `json:"hour,omitempty"`
	Day          string `json:"day,omitempty"`
	Month        string `json:"month,omitempty"`
	Weekday      string `json:"weekday,omitempty"`
	SpecialTime  string `json:"special_time,omitempty"`
	Disabled     bool   `json:"disabled"`
	Backup       bool   `json:"backup"`
	CronFile     string `json:"cron_file,omitempty"`
	Env          bool   `json:"env"`
	InsertAfter  string `json:"insertafter,omitempty"`
	InsertBefore string `json:"insertbefore,omitempty"`
}

type Response struct {
	Changed    bool     `json:"changed"`
	Failed     bool     `json:"failed"`
	Msg        string   `json:"msg,omitempty"`
	BackupFile string   `json:"backup_file,omitempty"`
	CronFile   string   `json:"cron_file,omitempty"`
	Jobs       []string `json:"jobs,omitempty"`
	Envs       []string `json:"envs,omitempty"`
}
