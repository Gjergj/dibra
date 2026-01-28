package git

type Request struct {
	Repo              string `json:"repo"`
	Dest              string `json:"dest,omitempty"`
	Version           string `json:"version,omitempty"`
	Remote            string `json:"remote,omitempty"`
	Clone             *bool  `json:"clone,omitempty"`
	Update            *bool  `json:"update,omitempty"`
	Force             bool   `json:"force,omitempty"`
	Depth             *int   `json:"depth,omitempty"`
	Bare              bool   `json:"bare,omitempty"`
	Recursive         *bool  `json:"recursive,omitempty"`
	TrackSubmodules   bool   `json:"track_submodules,omitempty"`
	SingleBranch      bool   `json:"single_branch,omitempty"`
	AcceptHostkey     bool   `json:"accept_hostkey,omitempty"`
	AcceptNewhostkey  bool   `json:"accept_newhostkey,omitempty"`
	KeyFile           string `json:"key_file,omitempty"`
	SSHOpts           string `json:"ssh_opts,omitempty"`
	Refspec           string `json:"refspec,omitempty"`
	Executable        string `json:"executable,omitempty"`
	SeparateGitDir    string `json:"separate_git_dir,omitempty"`
}

type Response struct {
	Changed          bool   `json:"changed"`
	Failed           bool   `json:"failed,omitempty"`
	Msg              string `json:"msg,omitempty"`
	Before           string `json:"before,omitempty"`
	After            string `json:"after,omitempty"`
	RemoteURLChanged bool   `json:"remote_url_changed,omitempty"`
}
