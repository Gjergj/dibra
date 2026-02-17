package cueconfig

import "embed"

//go:embed schema/*.cue
var schemaFS embed.FS
