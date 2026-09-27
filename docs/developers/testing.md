# Testing

*For a developer about to write or run tests. Covers what the checks are,
the helpers every app's tests build on, and the tests that guard the design.*

## The full check

Before every commit:

```bash
go build ./... && go vet ./... && go test ./... -race
```

This must stay green on every commit, and it takes under a minute. CI
([`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)) runs a stricter
version; to reproduce it exactly:

```bash
gofmt -l .                                              # must print nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...  # pinned, not @latest
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
```

CI also builds with `CGO_ENABLED=0` for linux/amd64, linux/arm64 and
darwin/arm64.

Running a subset while you work:

```bash
go test ./internal/apps/paste/... -run TestCreateThenView -v   # one test
go test ./internal/arch/                                       # the import rules
go test ./docs/ -v                                             # the link checker
```

## Store tests use a real SQLite file

Store tests open a real database file in `t.TempDir()`, never a mock or
`:memory:`. The bugs worth catching live in the SQL itself and in WAL
behaviour that `:memory:` doesn't reproduce. A store fixture applies the
platform schema and the app's own, exactly as the server does — from
[`internal/apps/paste/store_test.go`](../../internal/apps/paste/store_test.go):

```go
handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
if err != nil {
	t.Fatal(err)
}
t.Cleanup(func() { _ = handle.Close() })

// Apply the platform schema and this app's, exactly as the server does.
migrations, err := db.Collect(auth.Namespace, auth.Migrations())
// ...
appMigrations, err := db.Collect(paste.ID, paste.Migrations())
// ...
if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
	t.Fatal(err)
}
```

Fixtures here create two users, Alice and Bob, so every owner-scoping test
has somebody else's data to accidentally return.

## Handler tests: `apptest` and `htmlassert`

[`internal/apptest`](../../internal/apptest/apptest.go) builds the whole
stack for one app — real database, migrations, the production middleware
from `web.Stack`, the app mounted behind the real auth guard — with Alice and
Bob already signed in:

```go
s := apptest.NewServer(t, paste.New(), paste.NewStore)
```

The second argument turns the database into the app's store type, returned
as `s.Store` so a test can set up data directly. The server then gives you:

| Helper | Does |
| --- | --- |
| `s.Get(t, sess, path)` | GET, fail unless 200, return a parsed `*htmlassert.Doc` |
| `s.Do(t, sess, req)` | any request with the session's cookies (`nil` session = anonymous) |
| `s.Post(t, sess, path, form)` | a form POST with the CSRF token attached |
| `s.Submit(t, sess, path, form, wantLocation)` | a POST that must redirect to `wantLocation` |
| `s.PostHX` / `s.UploadHX` | the same, sent the way HTMX sends it |
| `s.Clock.Set(when)` / `s.Clock.Advance(d)` | pin "now" for the app and `s.Store` together |

[`internal/htmlassert`](../../internal/htmlassert/htmlassert.go) asserts on
the structure of the HTML, never on strings, so renaming a class or changing
whitespace doesn't break unrelated tests. It supports `tag`, `.class`,
`#id`, `[attr]`, `[attr=value]`, a tag with one of those attached, and
descendant selectors separated by spaces.

Two real examples from
[`internal/apps/paste/handlers_test.go`](../../internal/apps/paste/handlers_test.go),
where `newServer` wraps `apptest.NewServer` and `createSnippet` is a
paste-specific shortcut:

```go
func TestPasteRequiresSignIn(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/paste/", "/paste/new", "/paste/1", "/paste/raw/1"} {
		rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s anonymous = %d, want a 303 to the login page", path, rec.Code)
		}
	}
}

func TestCreatedAtUsesThePinnedClock(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))
	id := s.createSnippet(t, s.Alice, "My config", "yaml", "key: value\n")

	doc := s.Get(t, s.Alice, "/paste/"+itoa(id))
	if got := htmlassert.Text(doc.MustHave("#detail-view time")); got != "10 Mar 2026 12:00" {
		t.Errorf("created-at time = %q, want %q", got, "10 Mar 2026 12:00")
	}
}
```

About time: always control it with `s.Clock`. Don't compute "now" from the
real clock to line up with a handler, and don't call `s.Store.SetClock` in a
handler test — it doesn't reach the app's own store.

Both packages are ordinary (non-`_test.go`) packages so every app's tests
can import them; the arch test makes sure production code never does.

## Fast fixture passwords

Every fixture account stores `apptest.PasswordHash` (the hash of
`apptest.Password`) instead of calling `auth.HashPassword`. The production
Argon2id parameters (`m=64MiB, t=3, p=4`) cost about 27 ms per hash on
purpose; with hundreds of fixtures and log-ins, that was two thirds of the
suite's CPU time. The fixture hash uses `m=64KiB, t=1, p=1`, which took
`go test ./... -race -count=1` from 4m18s to 30s when it was introduced.

It's safe because a PHC hash string carries its own parameters, so
`VerifyPassword` checks it at the cheap cost while real passwords keep the
production cost — and `internal/platform/auth/password_test.go` still tests
`HashPassword` at the real parameters. Never use the cheap parameters for a
real password. If you build your own fixture user, store
`apptest.PasswordHash` too.

## The architecture test

[`internal/arch/arch_test.go`](../../internal/arch/arch_test.go) parses the
imports of every Go file in the repository and turns the design's rules into
failing tests:

| Test | Rule |
| --- | --- |
| `TestAppsDoNotImportEachOther` | an app never imports another app |
| `TestPlatformDoesNotImportApps` | `internal/platform/*` and `internal/ui` never import an app |
| `TestLayering` | the platform's internal order — see [Package layering](architecture.md#package-layering) |
| `TestUIIsALeaf` | `internal/ui` imports nothing from the module |
| `TestHTMLAssertIsTestOnly`, `TestAppTestIsTestOnly` | only `_test.go` files import the test helpers |
| `TestReadabilityIsContained` | go-readability is imported only by `internal/apps/reader/extract.go` |
| `TestFSRSIsContained` | go-fsrs is imported only by `internal/apps/flash/fsrs.go` |
| `TestRFC3339IsContained` | no stored time formatted as RFC 3339 outside a short allow-list |
| `TestAppsReadTheirStoreClock` | apps call `time.Now`/`Since`/`Until` only in their `store.go` |
| `TestScanSeesTheRealTree` | the scan itself found the packages it should |

If you move imports around anywhere under `internal/`, run
`go test ./internal/arch/` first — it tells you straight away if you've
crossed a boundary.

## The documentation link checker

[`docs/links_test.go`](../links_test.go) runs with `go test ./...` and checks
every relative link, image and `#anchor` in `README.md`, `CONTRIBUTING.md`
and the Markdown under `docs/user`, `docs/developers`, `docs/self-hosting`
and `docs/screenshots`. Anchors follow GitHub's heading slugs. Links inside
fenced code blocks are skipped; external `https://` links aren't fetched.
Guides under `docs/user/` must also stay plain Markdown, with no raw HTML
and no relative links that leave `docs/user/`, because they are meant to be
served inside the app as well (#309).

## Refreshing screenshots

How to regenerate the README and user-guide screenshots is described in
docs/screenshots/README.md, added with the screenshot tooling.

**Next:** [Releasing](releasing.md)
