package docker_context_info

type Request struct {
	OnlyCurrent bool   `json:"only_current"`
	Name        string `json:"name"`
	CLIContext  string `json:"cli_context"`
}

type Response struct {
	Changed            bool          `json:"changed"`
	Failed             bool          `json:"failed"`
	Msg                string        `json:"msg,omitempty"`
	Contexts           []ContextInfo `json:"contexts,omitempty"`
	CurrentContextName string        `json:"current_context_name,omitempty"`
}

type ContextInfo struct {
	Current     bool           `json:"current"`
	Name        string         `json:"name"`
	Description any            `json:"description"`
	MetaPath    *string        `json:"meta_path"`
	TLSPath     *string        `json:"tls_path"`
	Config      map[string]any `json:"config"`
}
