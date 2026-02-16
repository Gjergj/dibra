package schema

#Lineinfile: {
	#FilePerms
	path:           string
	line?:          string
	regexp?:        string
	search_string?: string
	state?:         string
	backrefs:       bool | *false
	insertafter?:   string
	insertbefore?:  string
	firstmatch:     bool | *false
	create:         bool | *false
	backup:         bool | *false
	validate?:      string
}

#Blockinfile: {
	#FilePerms
	path:             string
	block?:           string
	marker?:          string
	marker_begin?:    string
	marker_end?:      string
	insertafter?:     string
	insertbefore?:    string
	state?:           string
	create:           bool | *false
	backup:           bool | *false
	validate?:        string
	prepend_newline:  bool | *false
	append_newline:   bool | *false
}

#Replace: {
	#FilePerms
	path:      string
	regexp:    string
	replace?:  string
	after?:    string
	before?:   string
	backup:    bool | *false
	validate?: string
}
