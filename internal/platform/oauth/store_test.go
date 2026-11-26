package oauth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"authelia.com/provider/oauth2"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is one test's migrated database, two users and a pinned clock.
type fixture struct {
	s      *Store
	handle *sql.DB
	now    time.Time
	u1, u2 int64
}

// newFixture gives each test its own migrated database on disk (PATTERNS.md:
// real-SQLite-file store tests), two users, one registered client "c1", and
// a clock the test moves by assigning f.now.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatalf("db.Collect: %v", err)
	}
	if _, err := db.Apply(context.Background(), handle, ms); err != nil {
		t.Fatalf("db.Apply: %v", err)
	}

	f := &fixture{handle: handle, now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	f.s = NewStore(handle, func() time.Time { return f.now })
	f.s.setConfig(&oauth2.Config{})

	users := auth.NewStore(handle)
	for i, name := range []string{"alice", "bob"} {
		u, err := users.CreateUser(context.Background(), name, "$argon2id$fake", false)
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if i == 0 {
			f.u1 = u.ID
		} else {
			f.u2 = u.ID
		}
	}

	f.addClient(t, "c1")
	return f
}

func (f *fixture) addClient(t *testing.T, id string) {
	t.Helper()
	client := &oauth2.DefaultRegisteredClient{
		DefaultClient: &oauth2.DefaultClient{
			ID:           id,
			Public:       true,
			RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
			GrantTypes:   []string{"authorization_code", "refresh_token"},
			Scopes:       []string{"books:read"},
		},
		ClientName:              "Claude",
		TokenEndpointAuthMethod: "none",
	}
	if err := f.s.CreateClient(context.Background(), client); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
}

func (f *fixture) addGrant(t *testing.T, id string, userID int64, clientID string) Grant {
	t.Helper()
	g := Grant{
		ID: id, UserID: userID, ClientID: clientID, ClientName: "Claude",
		RedirectHost: "claude.ai",
		Requested:    []string{"books:read", "books:write"},
		Granted:      []string{"books:read"},
		CreatedAt:    f.now,
	}
	if err := f.s.UpsertGrant(context.Background(), g); err != nil {
		t.Fatalf("UpsertGrant: %v", err)
	}
	return g
}

// request builds what the library hands the store: a request whose ID is the
// grant ID and whose session carries every token's expiry.
func (f *fixture) request(id, clientID string) *oauth2.Request {
	r := oauth2.NewRequest()
	r.ID = id
	r.RequestedAt = f.now
	r.Client = &oauth2.DefaultClient{ID: clientID}
	r.SetRequestedScopes(oauth2.Arguments{"books:read", "books:write"})
	r.GrantScope("books:read")
	r.SetRequestedResource(oauth2.Arguments{"https://on.example/mcp"})
	r.GrantResource("https://on.example/mcp")
	r.Form.Set("redirect_uri", "https://claude.ai/api/mcp/auth_callback")
	r.Session = &oauth2.DefaultSession{
		Subject: "1",
		ExpiresAt: map[oauth2.TokenType]time.Time{
			oauth2.AuthorizeCode: f.now.Add(time.Minute),
			oauth2.AccessToken:   f.now.Add(time.Hour),
			oauth2.RefreshToken:  f.now.Add(90 * 24 * time.Hour),
		},
	}
	return r
}

func TestSecretsAreStableAndDistinct(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h1, r1, err := f.s.Secrets(ctx)
	if err != nil {
		t.Fatalf("Secrets: %v", err)
	}
	h2, r2, err := f.s.Secrets(ctx)
	if err != nil {
		t.Fatalf("Secrets (second): %v", err)
	}
	if len(h1) != 32 || len(r1) != 32 {
		t.Fatalf("len(hmac)=%d len(registration)=%d, want 32 each", len(h1), len(r1))
	}
	if !bytes.Equal(h1, h2) || !bytes.Equal(r1, r2) {
		t.Error("second call returned different secrets; they must be generated once and stored")
	}
	if bytes.Equal(h1, r1) {
		t.Error("hmac and registration secrets are equal; the library panics unless they differ")
	}
}

func TestUpsertGrantReplacesSameUserClient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	f.addGrant(t, "g2", f.u2, "c1")
	f.addGrant(t, "g3", f.u1, "c1")

	if _, err := f.s.Grant(ctx, "g1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grant(g1) err = %v, want ErrNotFound: a new grant for the same user and client replaces the old one", err)
	}
	g, err := f.s.Grant(ctx, "g3")
	if err != nil {
		t.Fatalf("Grant(g3): %v", err)
	}
	if g.UserID != f.u1 || g.ClientID != "c1" || g.Username != "alice" || g.RedirectHost != "claude.ai" {
		t.Errorf("Grant(g3) = %+v, want alice's c1 grant", g)
	}
	if len(g.Granted) != 1 || g.Granted[0] != "books:read" || len(g.Requested) != 2 {
		t.Errorf("scopes did not round-trip: requested %v granted %v", g.Requested, g.Granted)
	}
	if !g.CreatedAt.Equal(f.now) || !g.LastUsedAt.IsZero() {
		t.Errorf("CreatedAt=%v LastUsedAt=%v, want %v and zero", g.CreatedAt, g.LastUsedAt, f.now)
	}
	if _, err := f.s.Grant(ctx, "g2"); err != nil {
		t.Errorf("Grant(g2) for another user: %v, want it untouched", err)
	}
	all, err := f.s.Grants(ctx, 0)
	if err != nil || len(all) != 2 {
		t.Errorf("Grants(0) = %d grants, %v; want 2", len(all), err)
	}
	mine, err := f.s.Grants(ctx, f.u1)
	if err != nil || len(mine) != 1 || mine[0].ID != "g3" {
		t.Errorf("Grants(u1) = %+v, %v; want only g3", mine, err)
	}

	f.now = f.now.Add(time.Hour)
	if err := f.s.TouchGrant(ctx, "g3"); err != nil {
		t.Fatalf("TouchGrant: %v", err)
	}
	if g, _ := f.s.Grant(ctx, "g3"); !g.LastUsedAt.Equal(f.now) {
		t.Errorf("LastUsedAt = %v, want %v", g.LastUsedAt, f.now)
	}
}

func TestRevokeGrantDeletesTokensAndIsOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	if err := f.s.CreateAccessTokenSession(ctx, "at1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateAccessTokenSession: %v", err)
	}
	if err := f.s.CreateRefreshTokenSession(ctx, "rt1", "at1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}

	if err := f.s.RevokeGrant(ctx, f.u2, "g1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RevokeGrant by another user err = %v, want ErrNotFound", err)
	}
	if _, err := f.s.Grant(ctx, "g1"); err != nil {
		t.Errorf("grant gone after another user's revoke: %v", err)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "at1", nil); err != nil {
		t.Errorf("access token gone after another user's revoke: %v", err)
	}

	if err := f.s.RevokeGrant(ctx, 0, "g1"); err != nil {
		t.Fatalf("admin RevokeGrant: %v", err)
	}
	if _, err := f.s.Grant(ctx, "g1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grant after revoke err = %v, want ErrNotFound", err)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "at1", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("access token after revoke err = %v, want oauth2.ErrNotFound", err)
	}
	if _, err := f.s.GetRefreshTokenSession(ctx, "rt1", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("refresh token after revoke err = %v, want oauth2.ErrNotFound", err)
	}
	if err := f.s.RevokeGrant(ctx, f.u1, "g1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke err = %v, want ErrNotFound", err)
	}
}

func TestAuthorizeCodeInvalidateReturnsErrInvalidatedWithRequester(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	if err := f.s.CreateAuthorizeCodeSession(ctx, "code1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateAuthorizeCodeSession: %v", err)
	}
	// The library stores the PKCE session under the same signature.
	if err := f.s.CreatePKCERequestSession(ctx, "code1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreatePKCERequestSession under the code's signature: %v", err)
	}

	sess := new(oauth2.DefaultSession)
	req, err := f.s.GetAuthorizeCodeSession(ctx, "code1", sess)
	if err != nil {
		t.Fatalf("GetAuthorizeCodeSession: %v", err)
	}
	if req.GetID() != "g1" || req.GetClient().GetID() != "c1" || req.GetSession() != sess {
		t.Errorf("requester = id %q client %q, session hydrated into caller's: %v", req.GetID(), req.GetClient().GetID(), req.GetSession() == sess)
	}
	if sess.Subject != "1" || !sess.GetExpiresAt(oauth2.AccessToken).Equal(f.now.Add(time.Hour)) {
		t.Errorf("session = %+v, want subject and expiry restored", sess)
	}
	if !req.GetGrantedScopes().Has("books:read") || !req.GetGrantedResource().Has("https://on.example/mcp") ||
		len(req.GetRequestedScopes()) != 2 || req.GetRequestForm().Get("redirect_uri") == "" ||
		!req.GetRequestedAt().Equal(f.now) {
		t.Errorf("requester did not round-trip: %+v", req)
	}

	if err := f.s.InvalidateAuthorizeCodeSession(ctx, "code1"); err != nil {
		t.Fatalf("InvalidateAuthorizeCodeSession: %v", err)
	}
	req, err = f.s.GetAuthorizeCodeSession(ctx, "code1", new(oauth2.DefaultSession))
	if !errors.Is(err, oauth2.ErrInvalidatedAuthorizeCode) {
		t.Fatalf("after invalidate err = %v, want ErrInvalidatedAuthorizeCode", err)
	}
	if req == nil || req.GetID() != "g1" {
		t.Errorf("requester = %v, want it returned with the error so the grant can be revoked", req)
	}
	if _, err := f.s.GetPKCERequestSession(ctx, "code1", nil); err != nil {
		t.Errorf("PKCE session under the same signature: %v, want it unaffected", err)
	}
	if _, err := f.s.GetAuthorizeCodeSession(ctx, "nope", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("unknown code err = %v, want oauth2.ErrNotFound", err)
	}
	if err := f.s.CreateAuthorizeCodeSession(ctx, "code2", f.request("no-grant", "c1")); err == nil {
		t.Error("code for a request with no grant row was stored; consent must write the grant first")
	}
}

func TestRotateRefreshTokenThenGetReturnsInactiveWithRequester(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	if err := f.s.CreateAccessTokenSession(ctx, "at1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateAccessTokenSession: %v", err)
	}
	if err := f.s.CreateRefreshTokenSession(ctx, "rt1", "at1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}

	if err := f.s.RotateRefreshToken(ctx, "g1", "rt1"); err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	req, err := f.s.GetRefreshTokenSession(ctx, "rt1", new(oauth2.DefaultSession))
	if !errors.Is(err, oauth2.ErrInactiveToken) {
		t.Fatalf("after rotate err = %v, want ErrInactiveToken", err)
	}
	if req == nil || req.GetID() != "g1" {
		t.Errorf("requester = %v, want it returned with ErrInactiveToken", req)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "at1", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("access token after rotate err = %v, want it revoked", err)
	}
	if err := f.s.RotateRefreshToken(ctx, "g1", "rt1"); !errors.Is(err, oauth2.ErrInactiveToken) {
		t.Errorf("second rotate err = %v, want ErrInactiveToken", err)
	}
	if err := f.s.RotateRefreshToken(ctx, "g1", "nope"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("rotate unknown err = %v, want oauth2.ErrNotFound", err)
	}
}

func TestRevokeRefreshTokenRevokesTheWholeGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	f.addGrant(t, "g2", f.u2, "c1")
	for _, tok := range []struct{ sig, req string }{{"rt1", "g1"}, {"rt2", "g1"}, {"rt3", "g2"}} {
		if err := f.s.CreateAccessTokenSession(ctx, "a"+tok.sig, f.request(tok.req, "c1")); err != nil {
			t.Fatalf("CreateAccessTokenSession: %v", err)
		}
		if err := f.s.CreateRefreshTokenSession(ctx, tok.sig, "a"+tok.sig, f.request(tok.req, "c1")); err != nil {
			t.Fatalf("CreateRefreshTokenSession: %v", err)
		}
	}

	if err := f.s.RevokeRefreshToken(ctx, "g1"); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	if err := f.s.RevokeAccessToken(ctx, "g1"); err != nil {
		t.Fatalf("RevokeAccessToken: %v", err)
	}
	// Revoking ends the whole grant: the row and, by cascade, every token.
	if _, err := f.s.Grant(ctx, "g1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("grant g1 err = %v, want ErrNotFound", err)
	}
	if _, err := f.s.Grant(ctx, "g2"); err != nil {
		t.Errorf("other grant: %v, want still there", err)
	}
	for _, sig := range []string{"rt1", "rt2"} {
		if _, err := f.s.GetRefreshTokenSession(ctx, sig, nil); !errors.Is(err, oauth2.ErrNotFound) {
			t.Errorf("%s err = %v, want oauth2.ErrNotFound", sig, err)
		}
		if _, err := f.s.GetAccessTokenSession(ctx, "a"+sig, nil); !errors.Is(err, oauth2.ErrNotFound) {
			t.Errorf("a%s err = %v, want oauth2.ErrNotFound", sig, err)
		}
	}
	if _, err := f.s.GetRefreshTokenSession(ctx, "rt3", nil); err != nil {
		t.Errorf("other request's refresh token: %v, want still active", err)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "art3", nil); err != nil {
		t.Errorf("other request's access token: %v, want still there", err)
	}
}

func TestUpsertGrantNeverRewritesAnotherUsersGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.addGrant(t, "g1", f.u1, "c1")
	f.addGrant(t, "g2", f.u2, "c1")

	hijack := Grant{
		ID: "g1", UserID: f.u2, ClientID: "c1", ClientName: "Evil",
		RedirectHost: "evil.example", Requested: []string{"books:write"}, Granted: []string{"books:write"},
	}
	if err := f.s.UpsertGrant(ctx, hijack); err == nil {
		t.Error("UpsertGrant with another user's grant ID succeeded, want an error")
	}
	g, err := f.s.Grant(ctx, "g1")
	if err != nil {
		t.Fatalf("Grant(g1): %v", err)
	}
	if g.UserID != f.u1 || g.ClientName != "Claude" || g.RedirectHost != "claude.ai" || len(g.Granted) != 1 || g.Granted[0] != "books:read" {
		t.Errorf("g1 = %+v, want it untouched", g)
	}
	if _, err := f.s.Grant(ctx, "g2"); err != nil {
		t.Errorf("Grant(g2): %v, want the failed upsert's delete rolled back", err)
	}
}

func TestClientRoundTripAndDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	c, err := f.s.GetClient(ctx, "c1")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	rc, ok := c.(*oauth2.DefaultRegisteredClient)
	if !ok {
		t.Fatalf("GetClient returned %T, want *oauth2.DefaultRegisteredClient", c)
	}
	uris := rc.GetRedirectURIs()
	if len(uris) != 1 || uris[0] != "https://claude.ai/api/mcp/auth_callback" {
		t.Errorf("redirect URIs = %v", uris)
	}
	if !rc.IsPublic() || rc.TokenEndpointAuthMethod != "none" || rc.ClientName != "Claude" {
		t.Errorf("public=%v auth method=%q name=%q, want true, none, Claude", rc.IsPublic(), rc.TokenEndpointAuthMethod, rc.ClientName)
	}
	if _, err := f.s.GetClient(ctx, "nope"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("GetClient(unknown) err = %v, want oauth2.ErrNotFound", err)
	}

	if err := f.s.DeleteClient(ctx, "c1"); err != nil {
		t.Fatalf("DeleteClient: %v", err)
	}
	if _, err := f.s.GetClient(ctx, "c1"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("GetClient after delete err = %v, want oauth2.ErrNotFound", err)
	}
	// Counted directly: a Get would fail on the missing client either way.
	var n int
	if err := f.handle.QueryRow(`SELECT count(*) FROM oauth_sessions WHERE client_id = 'c1'`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("%d session rows left for the deleted client, want none", n)
	}
	if err := f.s.DeleteClient(ctx, "c1"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("second DeleteClient err = %v, want oauth2.ErrNotFound", err)
	}
}

func TestInTxReleasesTheConnectionOnPanic(t *testing.T) {
	f := newFixture(t)

	func() {
		defer func() { _ = recover() }()
		_ = f.s.inTx(context.Background(), func(querier) error { panic("boom") })
	}()

	// The database has one connection; a leaked transaction would hold it
	// and this read would time out.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := f.s.Grants(ctx, 0); err != nil {
		t.Fatalf("query after a panicking inTx: %v, want the connection released", err)
	}
}

func TestSetPKCEChallengeUsedIsOneShot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	exp := f.now.Add(time.Minute)

	if err := f.s.SetPKCEChallengeUsed(ctx, "c1", "chal", exp); err != nil {
		t.Fatalf("first SetPKCEChallengeUsed: %v", err)
	}
	if err := f.s.SetPKCEChallengeUsed(ctx, "c1", "chal", exp); !errors.Is(err, oauth2.ErrPKCEChallengeReused) {
		t.Errorf("second call err = %v, want ErrPKCEChallengeReused", err)
	}
	if err := f.s.SetPKCEChallengeUsed(ctx, "c2", "chal", exp); err != nil {
		t.Errorf("another client, same challenge: %v, want nil", err)
	}
	f.now = exp.Add(time.Second)
	if err := f.s.SetPKCEChallengeUsed(ctx, "c1", "chal", f.now.Add(time.Minute)); err != nil {
		t.Errorf("after the record expired: %v, want nil", err)
	}
}

func TestTransactionalRollbackDiscardsWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.addGrant(t, "g1", f.u1, "c1")

	txCtx, err := f.s.BeginTX(ctx)
	if err != nil {
		t.Fatalf("BeginTX: %v", err)
	}
	if err := f.s.CreateAccessTokenSession(txCtx, "at1", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateAccessTokenSession in tx: %v", err)
	}
	if _, err := f.s.GetAccessTokenSession(txCtx, "at1", nil); err != nil {
		t.Errorf("read inside the tx: %v, want its own write visible", err)
	}
	if err := f.s.Rollback(txCtx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "at1", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("after rollback err = %v, want oauth2.ErrNotFound", err)
	}

	txCtx, err = f.s.BeginTX(ctx)
	if err != nil {
		t.Fatalf("BeginTX: %v", err)
	}
	if err := f.s.CreateAccessTokenSession(txCtx, "at2", f.request("g1", "c1")); err != nil {
		t.Fatalf("CreateAccessTokenSession in tx: %v", err)
	}
	if err := f.s.Commit(txCtx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := f.s.GetAccessTokenSession(ctx, "at2", nil); err != nil {
		t.Errorf("after commit: %v, want the write kept", err)
	}
}

func TestSweepRemovesExpiredAndOldAudit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := f.now

	f.addGrant(t, "g1", f.u1, "c1")
	if err := f.s.CreateAuthorizeCodeSession(ctx, "code1", f.request("g1", "c1")); err != nil { // expires +1m
		t.Fatalf("CreateAuthorizeCodeSession: %v", err)
	}
	if err := f.s.CreateRefreshTokenSession(ctx, "rt1", "", f.request("g1", "c1")); err != nil { // expires +90d
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}
	if err := f.s.SetPKCEChallengeUsed(ctx, "c1", "chal", start.Add(time.Minute)); err != nil {
		t.Fatalf("SetPKCEChallengeUsed: %v", err)
	}
	if err := f.s.SetClientIDMetadataDocument(ctx, &oauth2.ClientIDMetadataDocumentRecord{
		ID: "https://app.example/client.json", Domain: "app.example",
		FetchedAt: start, ExpiresAt: start.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SetClientIDMetadataDocument: %v", err)
	}
	for _, at := range []time.Time{start.Add(-91 * 24 * time.Hour), start.Add(-89 * 24 * time.Hour)} {
		if err := f.s.Audit(ctx, AuditEntry{UserID: f.u1, ClientName: "Claude", Tool: "books_list", OK: true, At: at}); err != nil {
			t.Fatalf("Audit: %v", err)
		}
	}

	f.now = start.Add(2 * time.Hour)
	n, err := f.s.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 4 {
		t.Errorf("Sweep removed %d rows, want 4 (code, pkce marker, cimd record, old audit)", n)
	}
	if _, err := f.s.GetAuthorizeCodeSession(ctx, "code1", nil); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("expired code err = %v, want swept", err)
	}
	if _, err := f.s.GetRefreshTokenSession(ctx, "rt1", nil); err != nil {
		t.Errorf("unexpired refresh token: %v, want kept", err)
	}
	if _, err := f.s.GetClientIDMetadataDocument(ctx, "https://app.example/client.json"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("expired CIMD record err = %v, want swept", err)
	}
	if err := f.s.SetPKCEChallengeUsed(ctx, "c1", "chal", f.now.Add(time.Minute)); err != nil {
		t.Errorf("PKCE marker after sweep: %v, want it gone", err)
	}
	audit, err := f.s.RecentAudit(ctx, f.u1, 50)
	if err != nil {
		t.Fatalf("RecentAudit: %v", err)
	}
	if len(audit) != 1 || !audit[0].At.Equal(start.Add(-89*24*time.Hour)) || audit[0].Tool != "books_list" || !audit[0].OK {
		t.Errorf("audit after sweep = %+v, want only the 89-day-old row", audit)
	}
}

// A grant whose last access and refresh rows are gone is a dead connection:
// nothing can use it again, so the sweep drops it from /account. One still
// inside its first ten minutes may be mid-consent or mid-code-exchange and
// is kept.
func TestSweepRemovesDeadGrants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := f.now

	f.addGrant(t, "live-refresh", f.u1, "c1")
	if err := f.s.CreateRefreshTokenSession(ctx, "rt", "", f.request("live-refresh", "c1")); err != nil {
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}
	f.addClient(t, "c2")
	f.addGrant(t, "live-access", f.u1, "c2")
	if err := f.s.CreateAccessTokenSession(ctx, "at", f.request("live-access", "c2")); err != nil {
		t.Fatalf("CreateAccessTokenSession: %v", err)
	}
	f.addClient(t, "c3")
	f.addGrant(t, "dead", f.u1, "c3")
	f.addClient(t, "c4")
	f.addGrant(t, "expired", f.u2, "c4")
	expiring := f.request("expired", "c4")
	expiring.Session.SetExpiresAt(oauth2.AccessToken, start.Add(5*time.Minute))
	if err := f.s.CreateAccessTokenSession(ctx, "at-old", expiring); err != nil {
		t.Fatalf("CreateAccessTokenSession: %v", err)
	}
	// Created so it is exactly ten minutes old when the sweep runs: still
	// young enough to keep.
	f.now = start.Add(5 * time.Minute)
	f.addClient(t, "c5")
	f.addGrant(t, "young", f.u2, "c5")

	f.now = start.Add(15 * time.Minute)
	if _, err := f.s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, id := range []string{"live-refresh", "live-access", "young"} {
		if _, err := f.s.Grant(ctx, id); err != nil {
			t.Errorf("Grant(%s): %v, want kept", id, err)
		}
	}
	// "expired" loses its only token to the same sweep, so it goes too.
	for _, id := range []string{"dead", "expired"} {
		if _, err := f.s.Grant(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Grant(%s) err = %v, want swept", id, err)
		}
	}

	f.now = start.Add(15*time.Minute + time.Second)
	if _, err := f.s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := f.s.Grant(ctx, "young"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grant(young) one second past ten minutes err = %v, want swept", err)
	}
}

// A registered client nobody connected with within a day is noise from an
// abandoned or hostile registration; the sweep drops it. A client with a
// grant, or a younger one, is kept.
func TestSweepRemovesOrphanClients(t *testing.T) {
	f := newFixture(t) // c1, created at start
	ctx := context.Background()
	start := f.now

	f.addClient(t, "used")
	f.addGrant(t, "g-used", f.u1, "used")
	if err := f.s.CreateRefreshTokenSession(ctx, "rt", "", f.request("g-used", "used")); err != nil {
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}
	f.now = start.Add(time.Hour)
	f.addClient(t, "young")

	// c1 is exactly 24h old: kept.
	f.now = start.Add(24 * time.Hour)
	if _, err := f.s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	for _, id := range []string{"c1", "used", "young"} {
		if _, err := f.s.GetClient(ctx, id); err != nil {
			t.Errorf("GetClient(%s) at 24h: %v, want kept", id, err)
		}
	}

	f.now = start.Add(24*time.Hour + time.Second)
	if _, err := f.s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := f.s.GetClient(ctx, "c1"); !errors.Is(err, oauth2.ErrNotFound) {
		t.Errorf("GetClient(c1) err = %v, want swept", err)
	}
	for _, id := range []string{"used", "young"} {
		if _, err := f.s.GetClient(ctx, id); err != nil {
			t.Errorf("GetClient(%s): %v, want kept", id, err)
		}
	}
}

// A personal token never has access or refresh rows, so the dead-grant rule
// must leave it alone.
func TestSweepKeepsPersonalTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, hash := f.addPersonal(t, f.u1)
	f.now = f.now.Add(time.Hour)
	if _, err := f.s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := f.s.PersonalTokenGrant(ctx, hash); err != nil {
		t.Errorf("PersonalTokenGrant after sweep: %v, want kept", err)
	}
}

// A personal token nobody has used for 90 days, counted from its last use
// or from creation if never used, is removed.
func TestSweepRemovesIdlePersonalTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	start := f.now

	// An OAuth connection whose refresh token outlives the test.
	f.addGrant(t, "oauth", f.u2, "c1")
	req := f.request("oauth", "c1")
	req.Session.SetExpiresAt(oauth2.RefreshToken, start.Add(365*24*time.Hour))
	if err := f.s.CreateRefreshTokenSession(ctx, "rt", "", req); err != nil {
		t.Fatalf("CreateRefreshTokenSession: %v", err)
	}

	idA, _ := f.addPersonal(t, f.u1)
	idB, _ := f.addPersonal(t, f.u1)
	idC, _ := f.addPersonal(t, f.u1)
	touchAt := func(id string, at time.Time) {
		t.Helper()
		f.now = at
		if err := f.s.TouchGrant(ctx, id); err != nil {
			t.Fatalf("TouchGrant: %v", err)
		}
	}
	touchAt(idB, start.Add(24*time.Hour))
	touchAt(idC, start.Add(48*time.Hour))

	sweepAt := func(at time.Time) {
		t.Helper()
		f.now = at
		if _, err := f.s.Sweep(ctx); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
	}
	kept := func(id string, want bool) {
		t.Helper()
		_, err := f.s.Grant(ctx, id)
		if want && err != nil {
			t.Errorf("Grant(%s): %v, want kept", id, err)
		}
		if !want && !errors.Is(err, ErrNotFound) {
			t.Errorf("Grant(%s) err = %v, want swept", id, err)
		}
	}

	sweepAt(start.Add(90*24*time.Hour + time.Minute))
	kept(idA, false)
	kept(idB, true)
	kept(idC, true)
	kept("oauth", true)

	sweepAt(start.Add(91*24*time.Hour + time.Minute))
	kept(idB, false)
	kept(idC, true)
	kept("oauth", true)
}
