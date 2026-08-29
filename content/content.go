// Package content embeds the lesson sources.
package content

import "embed"

//go:embed lessons
var Lessons embed.FS
