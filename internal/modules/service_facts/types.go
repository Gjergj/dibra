package service_facts

type Request struct {
}

type ServiceInfo struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Status string `json:"status,omitempty"`
	Source string `json:"source"`
}

type Response struct {
	Changed  bool                   `json:"changed"`
	Failed   bool                   `json:"failed"`
	Msg      string                 `json:"msg,omitempty"`
	Services map[string]ServiceInfo `json:"services"`
}
