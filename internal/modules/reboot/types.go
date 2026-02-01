package reboot

type Request struct {
	PreRebootDelay  int      `json:"pre_reboot_delay"`
	PostRebootDelay int      `json:"post_reboot_delay"`
	RebootTimeout   int      `json:"reboot_timeout"`
	ConnectTimeout  int      `json:"connect_timeout"`
	TestCommand     string   `json:"test_command"`
	Msg             string   `json:"msg"`
	SearchPaths     []string `json:"search_paths"`
	BootTimeCommand string   `json:"boot_time_command"`
	RebootCommand   string   `json:"reboot_command"`
}

type Response struct {
	Changed  bool   `json:"changed"`
	Failed   bool   `json:"failed"`
	Msg      string `json:"msg,omitempty"`
	Rebooted bool   `json:"rebooted"`
	Elapsed  int    `json:"elapsed"`
}

func (r *Request) SetDefaults() {
	if r.RebootTimeout == 0 {
		r.RebootTimeout = 600
	}
	if r.TestCommand == "" {
		r.TestCommand = "whoami"
	}
	if r.Msg == "" {
		r.Msg = "Reboot initiated by GoAnsible"
	}
	if len(r.SearchPaths) == 0 {
		r.SearchPaths = []string{"/sbin", "/bin", "/usr/sbin", "/usr/bin", "/usr/local/sbin"}
	}
	if r.BootTimeCommand == "" {
		r.BootTimeCommand = "cat /proc/sys/kernel/random/boot_id"
	}
}
