package oauth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// personalGrant is a personal-token grant as the Account page creates one.
func (f *fixture) personalGrant(id string, userID int64) Grant {
	return Grant{
		ID: id, UserID: userID, ClientID: PersonalClientID, ClientName: "Pebble ring",
		RedirectHost: "",
		Requested:    []string{"notes:read", "notes:write"},
		Granted:      []string{"notes:read", "notes:write"},
		CreatedAt:    f.now,
	}
}

// addPersonal stores a new personal token for userID and returns its grant
// ID and hash.
func (f *fixture) addPersonal(t *testing.T, userID int64) (id, hash string) {
	t.Helper()
	id, err := newGrantID()
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err = newPersonalToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.CreatePersonalToken(context.Background(), f.personalGrant(id, userID), hash); err != nil {
		t.Fatalf("CreatePersonalToken: %v", err)
	}
	return id, hash
}

var lowerHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestNewPersonalTokenFormat(t *testing.T) {
	token, hash, err := newPersonalToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "onst_") || len(token) != 5+43 {
		t.Errorf("token = %q (len %d), want onst_ + 43 characters", token, len(token))
	}
	if hash != hashPersonalToken(token) {
		t.Errorf("hash %q != hashPersonalToken(token) %q", hash, hashPersonalToken(token))
	}
	if !lowerHex64.MatchString(hash) {
		t.Errorf("hash = %q, want 64 lowercase hex characters", hash)
	}
	other, _, err := newPersonalToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == token {
		t.Error("two calls gave the same token")
	}
}

func TestCreatePersonalTokenRoundTrip(t *testing.T) {
	f := newFixture(t)
	id, hash := f.addPersonal(t, f.u1)
	g, err := f.s.PersonalTokenGrant(context.Background(), hash)
	if err != nil {
		t.Fatalf("PersonalTokenGrant: %v", err)
	}
	if g.ID != id || g.UserID != f.u1 || g.ClientID != "personal" || g.ClientName != "Pebble ring" {
		t.Errorf("grant = %+v", g)
	}
	if strings.Join(g.Granted, " ") != "notes:read notes:write" {
		t.Errorf("granted = %v", g.Granted)
	}
	if !g.LastUsedAt.IsZero() {
		t.Errorf("last used = %v, want zero", g.LastUsedAt)
	}
}

func TestPersonalTokenGrantUnknownIsNotFound(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.PersonalTokenGrant(context.Background(), hashPersonalToken("onst_nope")); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSecondPersonalTokenKeepsTheFirst(t *testing.T) {
	f := newFixture(t)
	_, first := f.addPersonal(t, f.u1)
	_, second := f.addPersonal(t, f.u1)
	for _, h := range []string{first, second} {
		if _, err := f.s.PersonalTokenGrant(context.Background(), h); err != nil {
			t.Errorf("PersonalTokenGrant: %v, want both kept", err)
		}
	}
}

func TestPersonalTokenLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.addGrant(t, "oauth", f.u1, "c1") // doesn't count toward the ten
	for range maxPersonalTokens {
		f.addPersonal(t, f.u1)
	}
	_, hash, _ := newPersonalToken()
	err := f.s.CreatePersonalToken(ctx, f.personalGrant("eleventh", f.u1), hash)
	if !errors.Is(err, ErrTooManyTokens) {
		t.Fatalf("eleventh = %v, want ErrTooManyTokens", err)
	}
	if _, err := f.s.Grant(ctx, "eleventh"); !errors.Is(err, ErrNotFound) {
		t.Errorf("eleventh grant stored: %v", err)
	}
	if _, err := f.s.PersonalTokenGrant(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("eleventh token stored: %v", err)
	}
	f.addPersonal(t, f.u2)
}

func TestCreatePersonalTokenDuplicateID(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id, first := f.addPersonal(t, f.u1)
	_, second, _ := newPersonalToken()
	if err := f.s.CreatePersonalToken(ctx, f.personalGrant(id, f.u1), second); !errors.Is(err, ErrGrantExists) {
		t.Fatalf("duplicate = %v, want ErrGrantExists", err)
	}
	if _, err := f.s.PersonalTokenGrant(ctx, first); err != nil {
		t.Errorf("first token: %v, want kept", err)
	}
	if _, err := f.s.PersonalTokenGrant(ctx, second); !errors.Is(err, ErrNotFound) {
		t.Errorf("second token stored: %v", err)
	}
	// Another user can't take over the ID either.
	if err := f.s.CreatePersonalToken(ctx, f.personalGrant(id, f.u2), second); !errors.Is(err, ErrGrantExists) {
		t.Errorf("other user's duplicate = %v, want ErrGrantExists", err)
	}
}

func TestPersonalTokenGoesWithGrant(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(f *fixture, id string) error{
		"revoke": func(f *fixture, id string) error { return f.s.RevokeGrant(ctx, f.u1, id) },
		"delete user": func(f *fixture, _ string) error {
			_, err := f.handle.Exec(`DELETE FROM users WHERE id = ?`, f.u1)
			return err
		},
		"password change": func(f *fixture, _ string) error {
			return auth.NewStore(f.handle).SetPassword(ctx, f.u1, "$argon2id$other", "")
		},
	}
	for name, remove := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			id, hash := f.addPersonal(t, f.u1)
			if err := remove(f, id); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.PersonalTokenGrant(ctx, hash); !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
			var n int
			if err := f.handle.QueryRow(`SELECT count(*) FROM oauth_personal_tokens`).Scan(&n); err != nil || n != 0 {
				t.Errorf("token rows = %d (%v), want 0", n, err)
			}
		})
	}
}

func TestValidGrantID(t *testing.T) {
	id, err := newGrantID()
	if err != nil {
		t.Fatal(err)
	}
	if !validGrantID(id) {
		t.Errorf("validGrantID(%q) = false", id)
	}
	for _, bad := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.ToUpper(strings.Repeat("ab", 16)), "../../etc"} {
		if validGrantID(bad) {
			t.Errorf("validGrantID(%q) = true", bad)
		}
	}
	other, _ := newGrantID()
	if other == id {
		t.Error("two calls gave the same ID")
	}
}
