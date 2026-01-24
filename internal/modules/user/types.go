package user

type Request struct {
	Name           string   `json:"name"`
	State          string   `json:"state,omitempty"`
	UID            *int     `json:"uid,omitempty"`
	Group          string   `json:"group,omitempty"`
	Groups         []string `json:"groups,omitempty"`
	Append         bool     `json:"append"`
	Shell          string   `json:"shell,omitempty"`
	Home           string   `json:"home,omitempty"`
	CreateHome     *bool    `json:"create_home,omitempty"`
	MoveHome       bool     `json:"move_home"`
	System         bool     `json:"system"`
	Password       string   `json:"password,omitempty"`
	PasswordLock   *bool    `json:"password_lock,omitempty"`
	UpdatePassword string   `json:"update_password,omitempty"`
	Comment        string   `json:"comment,omitempty"`
	Expires        *float64 `json:"expires,omitempty"`
	Remove         bool     `json:"remove"`
	Force          bool     `json:"force"`
	Skeleton       string   `json:"skeleton,omitempty"`
	NonUnique      bool     `json:"non_unique"`

	GenerateSSHKey   bool   `json:"generate_ssh_key"`
	SSHKeyBits       int    `json:"ssh_key_bits,omitempty"`
	SSHKeyType       string `json:"ssh_key_type,omitempty"`
	SSHKeyFile       string `json:"ssh_key_file,omitempty"`
	SSHKeyComment    string `json:"ssh_key_comment,omitempty"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase,omitempty"`
}

type Response struct {
	Changed      bool   `json:"changed"`
	Failed       bool   `json:"failed"`
	Msg          string `json:"msg,omitempty"`
	Name         string `json:"name,omitempty"`
	UID          int    `json:"uid,omitempty"`
	GID          int    `json:"gid,omitempty"`
	Group        string `json:"group,omitempty"`
	Groups       string `json:"groups,omitempty"`
	Home         string `json:"home,omitempty"`
	Shell        string `json:"shell,omitempty"`
	Comment      string `json:"comment,omitempty"`
	State        string `json:"state,omitempty"`
	System       bool   `json:"system,omitempty"`
	CreateHome   bool   `json:"create_home,omitempty"`
	MoveHome     bool   `json:"move_home,omitempty"`
	SSHKeyFile   string `json:"ssh_key_file,omitempty"`
	SSHPublicKey string `json:"ssh_public_key,omitempty"`
	SSHFingerprint string `json:"ssh_fingerprint,omitempty"`
}
