package stat

type Request struct {
	Path   string `json:"path"`
	Follow bool   `json:"follow"`
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	Stat     *Stat  `json:"stat,omitempty"`
	Exists   bool   `json:"exists"`
}

type Stat struct {
	Exists   bool   `json:"exists"`
	IsDir    bool   `json:"isdir"`
	IsReg    bool   `json:"isreg"`
	IsLnk    bool   `json:"islnk"`
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Size     int64  `json:"size"`
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
	User     string `json:"user,omitempty"`
	Group    string `json:"group,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}
