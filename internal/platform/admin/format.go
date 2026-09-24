package admin

import (
	"fmt"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// humanBytes renders a byte count the way a person reads one. ON Paste has a
// near-identical helper: formatting is deliberately each package's own
// business (see app.Stat), and sharing six lines across the platform/app
// boundary would need a utility package this project does not have.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// appliedAtLabel shows a migration's applied_at as this page always has —
// RFC 3339 with trailing fractional zeros trimmed — although the database
// stores db.TimeLayout since #356. A value that does not parse is shown as
// stored, rather than hiding the row.
func appliedAtLabel(stored string) string {
	t, err := db.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.Format(time.RFC3339Nano)
}
