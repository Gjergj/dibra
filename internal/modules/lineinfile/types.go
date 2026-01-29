package lineinfile

type Request struct {
	Path         string `json:"path"`
	Line         string `json:"line,omitempty"`
	Regexp       string `json:"regexp,omitempty"`
	SearchString string `json:"search_string,omitempty"`
	State        string `json:"state,omitempty"`
	Backrefs     bool   `json:"backrefs"`
	InsertAfter  string `json:"insertafter,omitempty"`
	InsertBefore string `json:"insertbefore,omitempty"`
	FirstMatch   bool   `json:"firstmatch"`
	Create       bool   `json:"create"`
	Backup       bool   `json:"backup"`
	Mode         string `json:"mode,omitempty"`
	Owner        string `json:"owner,omitempty"`
	Group        string `json:"group,omitempty"`
	Validate     string `json:"validate,omitempty"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	Backup     string `json:"backup,omitempty"`
	Diff       *Diff  `json:"diff,omitempty"`
	Path       string `json:"path,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
}

type Diff struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}
