package blockinfile

type Request struct {
	Path           string `json:"path"`
	Block          string `json:"block,omitempty"`
	Marker         string `json:"marker,omitempty"`
	MarkerBegin    string `json:"marker_begin,omitempty"`
	MarkerEnd      string `json:"marker_end,omitempty"`
	InsertAfter    string `json:"insertafter,omitempty"`
	InsertBefore   string `json:"insertbefore,omitempty"`
	State          string `json:"state,omitempty"`
	Create         bool   `json:"create"`
	Backup         bool   `json:"backup"`
	Mode           string `json:"mode,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Group          string `json:"group,omitempty"`
	Validate       string `json:"validate,omitempty"`
	PrependNewline bool   `json:"prepend_newline"`
	AppendNewline  bool   `json:"append_newline"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	Path       string `json:"path,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
	Diff       *Diff  `json:"diff,omitempty"`
}

type Diff struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}
