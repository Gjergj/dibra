package systemd_service

type Request struct {
	Name         string `json:"name,omitempty"`
	State        string `json:"state,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
	Masked       *bool  `json:"masked,omitempty"`
	DaemonReload bool   `json:"daemon_reload"`
	DaemonReexec bool   `json:"daemon_reexec"`
	Scope        string `json:"scope,omitempty"`
	NoBlock      bool   `json:"no_block"`
	Force        bool   `json:"force"`
}

type Response struct {
	Changed bool              `json:"changed"`
	Failed  bool              `json:"failed"`
	Msg     string            `json:"msg,omitempty"`
	Name    string            `json:"name,omitempty"`
	State   string            `json:"state,omitempty"`
	Enabled bool              `json:"enabled,omitempty"`
	Status  map[string]string `json:"status,omitempty"`
}
