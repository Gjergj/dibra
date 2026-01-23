package apt_key

type Request struct {
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
	File     string `json:"file,omitempty"`
	Keyring  string `json:"keyring,omitempty"`
	ID       string `json:"id,omitempty"`
	State    string `json:"state"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	KeyID   string `json:"key_id,omitempty"`
	RC      int    `json:"rc"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}
