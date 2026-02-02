package apt_repository

type Request struct {
	Repo        string `json:"repo"`
	State       string `json:"state"`
	Filename    string `json:"filename,omitempty"`
	UpdateCache bool   `json:"update_cache"`
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Filename string `json:"filename,omitempty"`
	RC       int    `json:"rc"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}
