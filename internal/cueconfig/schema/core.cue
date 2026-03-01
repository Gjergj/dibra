package schema

#Ping: {
	data?: string
}

#GatherFacts: {
	gather_subset?: string | [...string]
	filter?:        string | [...string]
	fact_path?:     string
}

#Command: {
	cmd?:              string
	argv?:             [...string]
	chdir?:            string
	creates?:          string
	removes?:          string
	stdin?:            string
	stdin_add_newline?: bool
	strip_empty_ends?:  bool
}

#Shell: {
	cmd:               string
	chdir?:            string
	creates?:          string
	removes?:          string
	stdin?:            string
	stdin_add_newline?: bool
	strip_empty_ends?:  bool
	executable?:       string
}

#Script: {
	cmd:               string
	chdir?:            string
	creates?:          string
	removes?:          string
	executable?:       string
	strip_empty_ends?: bool
}

#Copy: {
	#FilePerms
	src?:        string
	dest:        string
	content?:    string
	backup:      bool | *false
	force:       bool | *false
	remote_src:  bool | *false
}

#Template: {
	src:                    string
	dest:                   string
	mode?:                  string
	owner?:                 string
	group?:                 string
	backup:                 bool | *false
	force?:                 bool
	follow:                 bool | *false
	validate?:              string
	newline_sequence?:      string
	variable_start_string?: string
	variable_end_string?:   string
	block_start_string?:    string
	block_end_string?:      string
	comment_start_string?:  string
	comment_end_string?:    string
	trim_blocks?:           bool
	lstrip_blocks?:         bool
}

#File: {
	path:     string
	state?:   string
	mode?:    string
	owner?:   string
	group?:   string
	src?:     string
	recurse:  bool | *false
	force:    bool | *false
	follow:   bool | *false
}

#Fetch: {
	src:               string
	dest:              string
	flat:              bool | *false
	fail_on_missing?:  bool
	validate_checksum?: bool
}

#Slurp: {
	src?:  string
	path?: string
}

#Tempfile: {
	path?:   string
	prefix?: string
	suffix?: string
	state?:  string
}

#Reboot: {
	pre_reboot_delay?:  int
	post_reboot_delay?: int
	reboot_timeout?:    int
	connect_timeout?:   int
	test_command?:      string
	msg?:               string
	search_paths?:      [...string]
	boot_time_command?: string
	reboot_command?:    string
}
