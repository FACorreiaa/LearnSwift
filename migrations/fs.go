// Package migrations carries the schema as embedded SQL.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
