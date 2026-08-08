package uri

type Request struct {
	URL             string            `json:"url"`
	Method          string            `json:"method,omitempty"`
	Body            interface{}       `json:"body,omitempty"`
	BodyFormat      string            `json:"body_format,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	StatusCode      []int             `json:"status_code,omitempty"`
	Timeout         int               `json:"timeout,omitempty"`
	ReturnContent   bool              `json:"return_content"`
	Dest            string            `json:"dest,omitempty"`
	Creates         string            `json:"creates,omitempty"`
	URLUsername     string            `json:"url_username,omitempty"`
	URLPassword     string            `json:"url_password,omitempty"`
	ForceBasicAuth  bool              `json:"force_basic_auth"`
	FollowRedirects string            `json:"follow_redirects,omitempty"`
	ValidateCerts   *bool             `json:"validate_certs,omitempty"`
}

type Response struct {
	Changed       bool              `json:"changed"`
	Failed        bool              `json:"failed"`
	Msg           string            `json:"msg,omitempty"`
	Status        int               `json:"status"`
	URL           string            `json:"url,omitempty"`
	Content       string            `json:"content,omitempty"`
	JSON          interface{}       `json:"json,omitempty"`
	ContentLength int64             `json:"content_length,omitempty"`
	ContentType   string            `json:"content_type,omitempty"`
	Redirected    bool              `json:"redirected"`
	Elapsed       int               `json:"elapsed"`
	Headers       map[string]string `json:"headers,omitempty"`
	Path          string            `json:"path,omitempty"`
}
