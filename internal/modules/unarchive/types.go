package unarchive

type Request struct {
	Src       string   `json:"src"`
	Dest      string   `json:"dest"`
	RemoteSrc bool     `json:"remote_src"`
	Creates   string   `json:"creates,omitempty"`
	ListFiles bool     `json:"list_files"`
	Exclude   []string `json:"exclude,omitempty"`
	Include   []string `json:"include,omitempty"`
	KeepNewer bool     `json:"keep_newer"`
	ExtraOpts []string `json:"extra_opts,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Group     string   `json:"group,omitempty"`
	Checksum  string   `json:"checksum,omitempty"`
}

type Response struct {
	Changed bool     `json:"changed"`
	Failed  bool     `json:"failed"`
	Msg     string   `json:"msg,omitempty"`
	Dest    string   `json:"dest,omitempty"`
	Src     string   `json:"src,omitempty"`
	Files   []string `json:"files,omitempty"`
	Handler string   `json:"handler,omitempty"`
}
