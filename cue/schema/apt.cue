package schema

#Apt: {
	name:            _ // string or list of strings
	state:           string | *"present"
	update_cache:    bool | *false
	cache_valid_time: int | *0
	purge:           bool | *false
	force_apt_get:   bool | *false
	autoremove:      bool | *false
	upgrade?:        string
}

#AptKey: {
	url?:     string
	data?:    string
	file?:    string
	keyring?: string
	id?:      string
	state?:   string
}

#AptRepository: {
	repo:         string
	state?:       string
	filename?:    string
	update_cache: bool | *false
}
