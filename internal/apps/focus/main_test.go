package focus_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // LoadLocation must not depend on the test machine's zoneinfo
)

// TestMain pins the process's local zone to one well east of UTC, so the
// day boundary ON Focus follows (the server's local date) is exercised the
// same way on every machine. Mirrors ON Flash's main_test.go.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}
