package books_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // LoadLocation must not depend on the test machine's zoneinfo
)

// TestMain pins the process's local zone to one well east of UTC, as
// Reader's and Flash's tests do, so the local day ON Books dates readings
// with (#424) differs from the UTC day and the tests prove which one is used.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}
