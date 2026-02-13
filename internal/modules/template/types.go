package template

type Request struct {
	Src                string `json:"src"`
	Dest               string `json:"dest"`
	Content            string `json:"content"`
	Mode               string `json:"mode,omitempty"`
	Owner              string `json:"owner,omitempty"`
	Group              string `json:"group,omitempty"`
	Backup             bool   `json:"backup"`
	Force              bool   `json:"force"`
	Follow             bool   `json:"follow"`
	Validate           string `json:"validate,omitempty"`
	NewlineSequence    string `json:"newline_sequence,omitempty"`
	VariableStartString string `json:"variable_start_string,omitempty"`
	VariableEndString   string `json:"variable_end_string,omitempty"`
	BlockStartString    string `json:"block_start_string,omitempty"`
	BlockEndString      string `json:"block_end_string,omitempty"`
	CommentStartString  string `json:"comment_start_string,omitempty"`
	CommentEndString    string `json:"comment_end_string,omitempty"`
	TrimBlocks         *bool  `json:"trim_blocks,omitempty"`
	LstripBlocks       *bool  `json:"lstrip_blocks,omitempty"`
}

type Response struct {
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Msg        string `json:"msg,omitempty"`
	Dest       string `json:"dest,omitempty"`
	Src        string `json:"src,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	BackupFile string `json:"backup_file,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Group      string `json:"group,omitempty"`
	Size       int64  `json:"size,omitempty"`
}
