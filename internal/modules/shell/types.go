package shell

type Request struct {
	Cmd             string `json:"cmd,omitempty"`
	Chdir           string `json:"chdir,omitempty"`
	Creates         string `json:"creates,omitempty"`
	Removes         string `json:"removes,omitempty"`
	Stdin           string `json:"stdin,omitempty"`
	StdinAddNewline *bool  `json:"stdin_add_newline,omitempty"`
	StripEmptyEnds  *bool  `json:"strip_empty_ends,omitempty"`
	Executable      string `json:"executable,omitempty"`
}

type Response struct {
	Changed     bool     `json:"changed"`
	Failed      bool     `json:"failed,omitempty"`
	Msg         string   `json:"msg,omitempty"`
	Cmd         string   `json:"cmd"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	StdoutLines []string `json:"stdout_lines"`
	StderrLines []string `json:"stderr_lines"`
	RC          int      `json:"rc"`
	Start       string   `json:"start,omitempty"`
	End         string   `json:"end,omitempty"`
	Delta       string   `json:"delta,omitempty"`
}
