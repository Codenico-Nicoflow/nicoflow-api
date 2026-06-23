// Package migrations embeds the SQL files into the binary so they ship with the
// distroless image. go:embed must sit beside the files, hence this tiny package.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
