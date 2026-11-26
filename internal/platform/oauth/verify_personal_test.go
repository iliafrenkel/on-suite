package oauth

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// personal creates a personal token named "Pebble ring" for userID and
// returns it with its grant ID.
func (e *flowEnv) personal(t *testing.T, userID int64, scopes ...string) (token, grantID string) {
	t.Helper()
	token, hash, err := newPersonalToken()
	if err != nil {
		t.Fatal(err)
	}
	grantID, err = newGrantID()
	if err != nil {
		t.Fatal(err)
	}
	err = e.srv.store.CreatePersonalToken(context.Background(), Grant{
		ID: grantID, UserID: userID, ClientID: PersonalClientID, ClientName: "Pebble ring",
		Requested: scopes, Granted: scopes,
	}, hash)
	if err != nil {
		t.Fatalf("CreatePersonalToken: %v", err)
	}
	return token, grantID
}

func TestVerifyAcceptsPersonalToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		token, id := e.personal(t, e.user.ID, "notes:read", "notes:write")
		c, err := e.srv.Verify(context.Background(), token)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if c.UserID != e.user.ID || c.GrantID != id || c.ClientName != "Pebble ring" ||
			!slices.Equal(c.Scopes, []string{"notes:read", "notes:write"}) {
			t.Errorf("Caller = %+v", c)
		}
		if want := time.Now().Add(24 * time.Hour); !c.Expires.Equal(want) {
			t.Errorf("Expires = %v, want %v", c.Expires, want)
		}
	})
}

func TestVerifyPersonalTokenUnknownOrMalformed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		token, _ := e.personal(t, e.user.ID, "notes:read")
		changed := token[:len(token)-1] + "A"
		if strings.HasSuffix(token, "A") {
			changed = token[:len(token)-1] + "B"
		}
		for _, bad := range []string{"onst_", "onst_x", "onst_" + strings.Repeat("A", 43), changed} {
			if _, err := e.srv.Verify(context.Background(), bad); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify(%q) = %v, want ErrInvalidToken", bad, err)
			}
		}
	})
}

func TestVerifyPersonalTokenRevoked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		token, id := e.personal(t, e.user.ID, "notes:read")
		if err := e.srv.store.RevokeGrant(context.Background(), e.user.ID, id); err != nil {
			t.Fatal(err)
		}
		if _, err := e.srv.Verify(context.Background(), token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyPersonalTokenStorageErrorIsUnavailable(t *testing.T) {
	for _, table := range []string{"oauth_personal_tokens", "oauth_grants"} {
		t.Run(table, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newFlowEnv(t)
				token, _ := e.personal(t, e.user.ID, "notes:read")
				if _, err := e.handle.Exec(`ALTER TABLE ` + table + ` RENAME TO broken`); err != nil {
					t.Fatal(err)
				}
				_, err := e.srv.Verify(context.Background(), token)
				if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidToken) {
					t.Errorf("Verify = %v, want ErrUnavailable and not ErrInvalidToken", err)
				}
			})
		})
	}
}

func TestVerifyPersonalTokenTouchesLastUsedAtMostOnceAMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		token, id := e.personal(t, e.user.ID, "notes:read")
		verify := func() time.Time {
			t.Helper()
			if _, err := e.srv.Verify(context.Background(), token); err != nil {
				t.Fatal(err)
			}
			g, err := e.srv.store.Grant(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			return g.LastUsedAt
		}
		first := verify()
		if first.IsZero() {
			t.Fatal("last used not set by the first Verify")
		}
		time.Sleep(30 * time.Second)
		if got := verify(); !got.Equal(first) {
			t.Errorf("last used moved after 30s: %v → %v", first, got)
		}
		time.Sleep(31 * time.Second)
		if got := verify(); !got.After(first) {
			t.Errorf("last used not moved after 61s: still %v", got)
		}
	})
}

func TestVerifyPersonalTokenIsPerUser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		other, err := auth.NewStore(e.handle).CreateUser(context.Background(), "mia", apptest.PasswordHash, false)
		if err != nil {
			t.Fatal(err)
		}
		mine, _ := e.personal(t, e.user.ID, "notes:read")
		theirs, _ := e.personal(t, other.ID, "notes:read")
		for token, want := range map[string]int64{mine: e.user.ID, theirs: other.ID} {
			c, err := e.srv.Verify(context.Background(), token)
			if err != nil || c.UserID != want {
				t.Errorf("Verify = %+v, %v; want user %d", c, err, want)
			}
		}
	})
}

func TestVerifyOAuthTokenUnchangedWithPersonalTokensPresent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		e.personal(t, e.user.ID, "notes:read")
		access, _ := e.exchangeOK(t, e.connect(t, "books:read books:write", "books:read"))
		before := time.Now()
		c, err := e.srv.Verify(context.Background(), access)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		g, _ := e.grant(t)
		if c.GrantID != g.ID || c.ClientName != "Claude" || !slices.Equal(c.Scopes, []string{"books:read"}) {
			t.Errorf("Caller = %+v", c)
		}
		if want := before.Add(accessLifespan); c.Expires.Sub(want).Abs() > time.Second {
			t.Errorf("Expires = %v, want about %v", c.Expires, want)
		}
	})
}
