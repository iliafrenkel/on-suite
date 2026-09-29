package flash_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // LoadLocation must not depend on the test machine's zoneinfo
)

// TestMain pins the process's local zone to one well east of UTC, so the
// day boundary the app follows (the server's local date, #424) is exercised
// the same way on every machine rather than being UTC on CI and whatever the
// developer's laptop happens to be set to everywhere else.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}
