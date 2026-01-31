package tempfile

type Request struct {
	Path   string  `json:"path,omitempty"`
	Prefix *string `json:"prefix,omitempty"`
	Suffix string  `json:"suffix,omitempty"`
	State  string  `json:"state,omitempty"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Path    string `json:"path,omitempty"`
	State   string `json:"state,omitempty"`
}
