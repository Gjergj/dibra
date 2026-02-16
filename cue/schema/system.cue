package schema

#Cron: {
	name:          string
	user?:         string
	job?:          string
	state?:        string
	minute?:       string
	hour?:         string
	day?:          string
	month?:        string
	weekday?:      string
	special_time?: string
	disabled:      bool | *false
	backup:        bool | *false
	cron_file?:    string
	env:           bool | *false
	insertafter?:  string
	insertbefore?: string
}

#User: {
	name:               string
	state?:             string
	uid?:               int
	group?:             string
	groups?:            [...string]
	append:             bool | *false
	shell?:             string
	home?:              string
	create_home?:       bool
	move_home:          bool | *false
	system:             bool | *false
	password?:          string
	password_lock?:     bool
	update_password?:   string
	comment?:           string
	expires?:           number
	remove:             bool | *false
	force:              bool | *false
	skeleton?:          string
	non_unique:         bool | *false
	generate_ssh_key:   bool | *false
	ssh_key_bits?:      int
	ssh_key_type?:      string
	ssh_key_file?:      string
	ssh_key_comment?:   string
	ssh_key_passphrase?: string
}

#Group: {
	name:       string
	state?:     string
	gid?:       int
	system:     bool | *false
	local:      bool | *false
	non_unique: bool | *false
	force:      bool | *false
}

#URI: {
	url:              string
	method?:          string
	body?:            string
	body_format?:     string
	headers?:         {[string]: string}
	status_code?:     [...int]
	timeout?:         int
	return_content:   bool | *false
	dest?:            string
	creates?:         string
	url_username?:    string
	url_password?:    string
	force_basic_auth: bool | *false
	follow_redirects?: string
	validate_certs?:  bool
}

#Unarchive: {
	src:        string
	dest:       string
	remote_src: bool | *false
	creates?:   string
	list_files: bool | *false
	exclude?:   [...string]
	include?:   [...string]
	keep_newer: bool | *false
	extra_opts?: [...string]
	mode?:      string
	owner?:     string
	group?:     string
}

#Git: {
	repo:               string
	dest?:              string
	version?:           string
	remote?:            string
	clone?:             bool
	update?:            bool
	force:              bool | *false
	depth?:             int
	bare:               bool | *false
	recursive?:         bool
	track_submodules:   bool | *false
	single_branch:      bool | *false
	accept_hostkey:     bool | *false
	accept_newhostkey:  bool | *false
	key_file?:          string
	ssh_opts?:          string
	refspec?:           string
	executable?:        string
	separate_git_dir?:  string
}

#Find: {
	paths?:              [...string]
	path?:               [...string]
	name?:               [...string]
	patterns?:           [...string]
	pattern?:            [...string]
	excludes?:           [...string]
	exclude?:            [...string]
	contains?:           string
	read_whole_file:     bool | *false
	file_type?:          string
	age?:                string
	age_stamp?:          string
	size?:               string
	recurse:             bool | *false
	hidden:              bool | *false
	follow:              bool | *false
	get_checksum:        bool | *false
	checksum_algorithm?: string
	use_regex:           bool | *false
	depth?:              int
	mode?:               string
	exact_mode?:         bool
	limit?:              int
}

#UFW: {
	state?:               string
	logging?:             string
	default?:             string
	policy?:              string
	direction?:           string
	rule?:                string
	delete:               bool | *false
	insert?:              int
	insert_relative_to?:  string
	interface?:           string
	if?:                  string
	interface_in?:        string
	if_in?:               string
	interface_out?:       string
	if_out?:              string
	from_ip?:             string
	from?:                string
	src?:                 string
	from_port?:           string
	to_ip?:               string
	dest?:                string
	to?:                  string
	to_port?:             string
	port?:                string
	proto?:               string
	protocol?:            string
	name?:                string
	app?:                 string
	route:                bool | *false
	log:                  bool | *false
	comment?:             string
}
