package script

type Request struct {
	Cmd             string `json:"cmd,omitempty"`
	ScriptPath      string `json:"script_path,omitempty"`
	Args            string `json:"args,omitempty"`
	Chdir           string `json:"chdir,omitempty"`
	Creates         string `json:"creates,omitempty"`
	Removes         string `json:"removes,omitempty"`
	Executable      string `json:"executable,omitempty"`
	StripEmptyEnds  *bool  `json:"strip_empty_ends,omitempty"`
}

type Response struct {
	Changed     bool     `json:"changed"`
	Failed      bool     `json:"failed,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
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
