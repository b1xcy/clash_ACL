package assets

import "embed"

//go:embed rules
var Rules embed.FS

//go:embed all:dist
var Web embed.FS
