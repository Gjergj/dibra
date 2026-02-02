package copy

type Request struct {
	Src       string `json:"src,omitempty"`
	Dest      string `json:"dest"`
	Content   string `json:"content,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Group     string `json:"group,omitempty"`
	Backup    bool   `json:"backup"`
	Force     bool   `json:"force"`
	RemoteSrc bool   `json:"remote_src"`
	Checksum  string `json:"checksum,omitempty"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	Dest       string `json:"dest,omitempty"`
	Src        string `json:"src,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Group      string `json:"group,omitempty"`
	Size       int64  `json:"size,omitempty"`
}
