package apt

type Request struct {
	Packages       []string `json:"packages,omitempty"`
	State          string   `json:"state"`
	UpdateCache    bool     `json:"update_cache"`
	CacheValidTime int      `json:"cache_valid_time"`
	Purge          bool     `json:"purge"`
	Autoremove     bool     `json:"autoremove"`
	Upgrade        string   `json:"upgrade"`
}

type Response struct {
	Changed  bool     `json:"changed"`
	Failed   bool     `json:"failed"`
	Msg      string   `json:"msg,omitempty"`
	RC       int      `json:"rc"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Packages []string `json:"packages,omitempty"`
}
