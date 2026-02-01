package replace

type Request struct {
	Path    string `json:"path"`
	Regexp  string `json:"regexp"`
	Replace string `json:"replace,omitempty"`
	After   string `json:"after,omitempty"`
	Before  string `json:"before,omitempty"`
	Backup  bool   `json:"backup"`
	Mode    string `json:"mode,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Group   string `json:"group,omitempty"`
	Validate string `json:"validate,omitempty"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed,omitempty"`
	Msg        string `json:"msg,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
	Diff       *Diff  `json:"diff,omitempty"`
}

type Diff struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}
