// Package migrations embeds the SQL migration files into the binary so they
// travel with the distroless image — no `migrate` CLI, shell, or on-disk files
// are needed at runtime. The `go:embed` directive must live in the same
// directory as the files it embeds, which is why this tiny package sits beside
// the .sql files rather than under internal/.
package migrations

import "embed"

// FS holds every up/down migration. Consumed by internal/db.Migrate via the
// golang-migrate iofs source driver.
//
//go:embed *.sql
var FS embed.FS
