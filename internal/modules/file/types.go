package file

type Request struct {
	Path    string `json:"path"`
	State   string `json:"state"`
	Mode    string `json:"mode,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Group   string `json:"group,omitempty"`
	Src     string `json:"src,omitempty"`
	Recurse bool   `json:"recurse"`
	Force   bool   `json:"force"`
	Follow  bool   `json:"follow"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Path    string `json:"path,omitempty"`
	State   string `json:"state,omitempty"`
	Dest    string `json:"dest,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Group   string `json:"group,omitempty"`
	Mode    string `json:"mode,omitempty"`
}
