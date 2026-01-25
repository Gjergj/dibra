package service

type Request struct {
	Name      string `json:"name"`
	State     string `json:"state,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Sleep     int    `json:"sleep,omitempty"`
	Use       string `json:"use,omitempty"`
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
