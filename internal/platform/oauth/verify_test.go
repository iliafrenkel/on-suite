package oauth

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

func TestVerifyAcceptsLiveAccessToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		access, _ := e.exchangeOK(t, e.connect(t, "books:read books:write", "books:read"))
		before := time.Now()

		c, err := e.srv.Verify(context.Background(), access)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		g, _ := e.grant(t)
		if c.UserID != e.user.ID || c.GrantID != g.ID || c.ClientName != "Claude" || !slices.Equal(c.Scopes, []string{"books:read"}) {
			t.Errorf("Caller = %+v", c)
		}
		if want := before.Add(accessLifespan); c.Expires.Sub(want).Abs() > time.Second {
			t.Errorf("Expires = %v, want about %v", c.Expires, want)
		}
		if g.LastUsedAt.IsZero() {
			t.Error("last_used_at not set by Verify")
		}
	})
}

func TestVerifyRefusesRefreshToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		_, refresh := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		if _, err := e.srv.Verify(context.Background(), refresh); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify(refresh token) = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyRefusesOtherResource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		access, _ := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		forge := func(resource string) {
			t.Helper()
			if _, err := e.handle.Exec(`UPDATE oauth_sessions SET data = CAST(json_set(CAST(data AS TEXT), '$.granted_resource', json(?)) AS BLOB) WHERE kind = 'access'`,
				`["`+resource+`"]`); err != nil {
				t.Fatal(err)
			}
		}
		// Positive control: the same rewrite with our own resource is
		// accepted, so the refusal below is down to the resource alone.
		forge("https://onsuite.test/mcp")
		if _, err := e.srv.Verify(context.Background(), access); err != nil {
			t.Fatalf("Verify with our resource: %v", err)
		}
		forge("https://other.example/mcp")
		if _, err := e.srv.Verify(context.Background(), access); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyRefusesAuthorizationCode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		code := e.connect(t, "books:read", "books:read")
		if _, err := e.srv.Verify(context.Background(), code); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify(authorization code) = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyRefusesExpiredAndRevoked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		access, _ := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		if _, err := e.srv.Verify(context.Background(), access); err != nil {
			t.Fatalf("Verify before expiry: %v", err)
		}
		time.Sleep(61 * time.Minute)
		if _, err := e.srv.Verify(context.Background(), access); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify after 61 minutes = %v, want ErrInvalidToken", err)
		}

		access, _ = e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		g, _ := e.grant(t)
		if err := e.srv.store.RevokeGrant(context.Background(), e.user.ID, g.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.srv.Verify(context.Background(), access); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify after revoke = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyRefusesDeletedUser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		access, _ := e.exchangeOK(t, e.connect(t, "books:read", "books:read"))
		if err := auth.NewStore(e.handle).DeleteUser(context.Background(), e.user.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.srv.Verify(context.Background(), access); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify = %v, want ErrInvalidToken", err)
		}
	})
}

func TestVerifyRefusesGarbage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newFlowEnv(t)
		for _, tok := range []string{"", "nope", "ory_at_x.y"} {
			if _, err := e.srv.Verify(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify(%q) = %v, want ErrInvalidToken", tok, err)
			}
		}
	})
}
