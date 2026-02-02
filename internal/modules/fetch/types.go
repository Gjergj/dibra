package fetch

type Request struct {
	Src              string `json:"src"`
	Dest             string `json:"dest"`
	Flat             bool   `json:"flat"`
	FailOnMissing    bool   `json:"fail_on_missing"`
	ValidateChecksum bool   `json:"validate_checksum"`
}

type Response struct {
	Changed        bool   `json:"changed"`
	Failed         bool   `json:"failed"`
	Msg            string `json:"msg,omitempty"`
	Dest           string `json:"dest,omitempty"`
	File           string `json:"file,omitempty"`
	Checksum       string `json:"checksum,omitempty"`
	RemoteChecksum string `json:"remote_checksum,omitempty"`
}
