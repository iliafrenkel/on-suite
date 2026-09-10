package apptest_test

import (
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// PasswordHash is a hand-generated constant, so nothing else would notice if it
// stopped matching Password — every fixture user would simply fail to log in,
// and the failure would surface as a confusing 401 in some unrelated app's
// handler test rather than here.
func TestPasswordHashMatchesPassword(t *testing.T) {
	ok, err := auth.VerifyPassword(apptest.PasswordHash, apptest.Password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v — PasswordHash is not a usable PHC string", err)
	}
	if !ok {
		t.Fatal("PasswordHash does not match Password; regenerate it with the snippet in its doc comment")
	}
}

// The cheap parameters are the whole point, so pin them: someone pasting a
// production-parameter hash in here would silently give back the 4m18s suite.
func TestPasswordHashUsesCheapParameters(t *testing.T) {
	const want = "$argon2id$v=19$m=64,t=1,p=1$"
	if len(apptest.PasswordHash) < len(want) || apptest.PasswordHash[:len(want)] != want {
		t.Errorf("PasswordHash parameters changed; want the cheap %q prefix, got %q",
			want, apptest.PasswordHash)
	}
}
