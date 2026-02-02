package ping

type Request struct {
	Data string `json:"data"`
}

type Response struct {
	Changed bool   `json:"changed"`
	Failed  bool   `json:"failed"`
	Msg     string `json:"msg,omitempty"`
	Ping    string `json:"ping,omitempty"`
}
