package slurp

type Request struct {
	Src string `json:"src"`
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	Content  string `json:"content,omitempty"`
	Source   string `json:"source,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}
