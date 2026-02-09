package find

type Request struct {
	Paths             []string `json:"paths"`
	Patterns          []string `json:"patterns,omitempty"`
	Excludes          []string `json:"excludes,omitempty"`
	Contains          string   `json:"contains,omitempty"`
	ReadWholeFile     bool     `json:"read_whole_file,omitempty"`
	FileType          string   `json:"file_type,omitempty"`
	Age               string   `json:"age,omitempty"`
	AgeStamp          string   `json:"age_stamp,omitempty"`
	Size              string   `json:"size,omitempty"`
	Recurse           bool     `json:"recurse,omitempty"`
	Hidden            bool     `json:"hidden,omitempty"`
	Follow            bool     `json:"follow,omitempty"`
	GetChecksum       bool     `json:"get_checksum,omitempty"`
	ChecksumAlgorithm string   `json:"checksum_algorithm,omitempty"`
	UseRegex          bool     `json:"use_regex,omitempty"`
	Depth             int      `json:"depth,omitempty"`
	Mode              string   `json:"mode,omitempty"`
	ExactMode         bool     `json:"exact_mode,omitempty"`
	Limit             int      `json:"limit,omitempty"`
}

type FileInfo struct {
	Path     string  `json:"path"`
	Mode     string  `json:"mode"`
	IsDir    bool    `json:"isdir"`
	IsChr    bool    `json:"ischr"`
	IsBlk    bool    `json:"isblk"`
	IsReg    bool    `json:"isreg"`
	IsFIFO   bool    `json:"isfifo"`
	IsLnk    bool    `json:"islnk"`
	IsSock   bool    `json:"issock"`
	UID      int     `json:"uid"`
	GID      int     `json:"gid"`
	Size     int64   `json:"size"`
	Inode    uint64  `json:"inode"`
	Dev      uint64  `json:"dev"`
	Nlink    uint64  `json:"nlink"`
	Atime    float64 `json:"atime"`
	Mtime    float64 `json:"mtime"`
	Ctime    float64 `json:"ctime"`
	GrName   string  `json:"gr_name"`
	PwName   string  `json:"pw_name"`
	Wusr     bool    `json:"wusr"`
	Rusr     bool    `json:"rusr"`
	Xusr     bool    `json:"xusr"`
	Wgrp     bool    `json:"wgrp"`
	Rgrp     bool    `json:"rgrp"`
	Xgrp     bool    `json:"xgrp"`
	Woth     bool    `json:"woth"`
	Roth     bool    `json:"roth"`
	Xoth     bool    `json:"xoth"`
	IsUID    bool    `json:"isuid"`
	IsGID    bool    `json:"isgid"`
	Checksum string  `json:"checksum,omitempty"`
}

type Response struct {
	Changed      bool              `json:"changed"`
	Failed       bool              `json:"failed,omitempty"`
	Msg          string            `json:"msg"`
	Files        []FileInfo        `json:"files"`
	Matched      int               `json:"matched"`
	Examined     int               `json:"examined"`
	SkippedPaths map[string]string `json:"skipped_paths"`
}
