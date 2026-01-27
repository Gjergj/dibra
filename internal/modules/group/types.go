package group

type Request struct {
	Name      string `json:"name"`
	State     string `json:"state,omitempty"`
	GID       *int   `json:"gid,omitempty"`
	System    bool   `json:"system"`
	Local     bool   `json:"local"`
	NonUnique bool   `json:"non_unique"`
	Force     bool   `json:"force"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Name    string `json:"name,omitempty"`
	GID     int    `json:"gid,omitempty"`
	State   string `json:"state,omitempty"`
	System  bool   `json:"system,omitempty"`
}
