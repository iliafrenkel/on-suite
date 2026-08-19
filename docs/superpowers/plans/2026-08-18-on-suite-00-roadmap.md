# ON Suite — Implementation Roadmap

**Spec:** `docs/superpowers/specs/2026-08-18-on-suite-platform-design.md`

The spec's scope — the platform plus ON Paste — is delivered as three plans
rather than one. A single plan covering all 23 tasks at the fidelity this
project's plans use came to roughly 11,000 lines, which is not reviewable, and
its later tasks would have been written against guesses about code that does
not exist yet.

Each plan below produces working, testable software on its own, and each is
written only after the previous one has been executed and reviewed, so it can
absorb what was actually learned.

| Plan | File | Delivers |
|---|---|---|
| 1 | `2026-08-18-on-suite-01-platform-core.md` | Config, SQLite, migrations, password hashing, users, sessions, `onsuite user add`. **Written.** |
| 2 | `2026-08-18-on-suite-02-web-and-apps.md` | Assets, templates, middleware, CSRF, login, the app framework. **Written.** |
| 3 | `2026-08-18-on-suite-03-paste-and-ops.md` | ON Paste, backup, TLS, packaging, CI. **Written.** |

## Full task inventory

Tasks 1–7 are specified in full in Plan 1. Tasks 8–23 are listed here so the
scope is on record; each becomes a full task specification when its plan is
written.

### Plan 1 — Platform core (executed)

| # | Task | Notes |
|---|---|---|
| 1 | Module bootstrap, config, serve, `/healthz`, graceful shutdown | |
| 2 | Open SQLite with WAL, busy timeout, foreign keys; checkpoint and snapshot helpers | |
| 3 | Forward-only namespaced migration runner | |
| 4 | Argon2id password hashing | **TDD** |
| 5 | Identity schema and user store | |
| 6 | Sessions with throttled sliding expiry | **TDD** |
| 7 | `onsuite user add`; migrations applied on startup | |

**Deliverable:** a real account in a real database, created by the binary.

### Plan 2 — Web plumbing and the app framework (executed)

| # | Task | Notes |
|---|---|---|
| 8 | Vendored HTMX and Alpine-free CSS design tokens; embedded static handler with cache headers and content hashing | |
| 9 | Template renderer, base layout, `PageData`, the shell | |
| 10 | Recover, request logging and security-header middleware; error pages that respect HTMX requests | |
| 11 | Per-session CSRF issue and verify | **TDD** |
| 12 | Auth middleware, login and logout handlers, login rate limiting | **TDD** |
| 13 | `app.App` interface, `Deps`, and the default-deny `Router`; registry and app switcher | See note below |
| 14 | Import-boundary architecture test | |

**Deliverable:** log in with the account from Plan 1 and land on the shell,
with the app switcher rendering from the registry. Satisfies spec §11.3.

**Refinement to §7.4, now implemented in Plan 2 Task 13.** The spec describes the
auth middleware skipping a list of app-declared public routes. Plan 2 instead
gives apps a `Router` whose default `Handle` requires authentication and whose
explicit `Public` method does not. Both satisfy the spec's intent — only declared
routes are anonymous — but the skip-list fails *open* if an app forgets to
declare a route, while the wrapper fails *closed*. This is a strengthening of
§7.4, not a departure from it.

**Two findings from Plan 2 that Plan 3 must respect.**

1. **`ServeMux` panics at startup on ambiguous patterns.** Two patterns of equal
   segment count, one wildcard-first and one literal-first, collide:
   `GET /{slug}/raw` and `GET /s/{slug}` both match `/paste/s/raw`. ON Paste's
   route design must keep wildcards at distinct depths.
2. **A test can pass while asserting nothing.** Two such bugs were found in
   Plan 2 by running its own code — a selector that silently matched nothing and
   a scan that silently found no packages. Plan 3's tests should be checked
   against a deliberately broken implementation, not only against a working
   one.

### Plan 3 — ON Paste and operations (written)

| # | Task | Notes |
|---|---|---|
| 15 | Paste migration and snippet store | |
| 16 | Create and view a snippet, with Chroma syntax highlighting | |
| 17 | List and delete snippets, scoped to the owner | |
| 18 | Public share links via unguessable slug | |
| 19 | JSON export | |
| 20 | `onsuite backup`, nightly snapshot job, retention | |
| 21 | TLS: reverse-proxy mode and built-in `autocert` mode | |
| 22 | Packaging: goreleaser, Dockerfile, systemd unit, README and deploy docs | |
| 23 | CI: vet, staticcheck, race tests, cross-compile matrix | |

**Deliverable:** the full spec, including success criteria §11.4 through §11.8.

## What building the first real app taught us

ON Paste was the test of whether the platform holds up. It mostly did — the app
is a package under `internal/apps` plus one line in `registeredApps`, and it
appears in the nav and dashboard with no platform code naming it. Three things
surfaced:

1. **One genuine platform gap.** `base.html` gave a page no way to add anything
   to `<head>`, which any app with its own stylesheet needs. Fixed with a `head`
   block. This is the kind of change the spec said to watch for, and it added no
   knowledge of ON Paste to the platform.
2. **One latent crash.** `Registry.Mount` handed a nil filesystem to `fs.Glob`
   when an app did not implement `Templates()`, panicking instead of reporting
   the problem. Fixed with a clear error and a test.
3. **Drifted duplication in the commands.** `serve`, `user add` and the new
   `export` had each grown their own version of "open the database and set up the
   schema", and they disagreed about which migrations to apply. Unified behind
   `openDatabase`. Worth remembering when ON Notes adds commands of its own.

Also worth carrying forward: `ServeMux` panics at startup on ambiguous patterns,
and a strict CSP means any library emitting inline styles — Chroma's default
formatter, for one — must be reconfigured or it silently produces an unstyled
page.

## Deliberately not built

Recorded so these stay decisions rather than oversights:

- **Paste expiry.** The brainstorming sketch mentioned expiring snippets, but
  the spec's success criteria do not require it, so it is out. Adding an
  `expires_at` column later is one migration.
- **Editing a snippet.** Pastebins are conventionally immutable and the spec
  names only create, view, list and delete.
- Everything in spec §10: full-text search, a background job scheduler,
  uploads, metrics, Alpine.js, multi-tenant hardening. Each arrives with the
  app that first needs it.

## Beyond the spec, on purpose

Decided during planning rather than in the spec, and recorded here so they are
not mistaken for scope creep:

- **Raw text endpoints** (`/paste/raw/{id}` and `/paste/s/{slug}/raw`), because
  piping a snippet into a terminal is how a pastebin actually gets used.
- **Revocable sharing.** The spec asked only for an unguessable public link;
  re-sharing mints a fresh slug so a revoked link stays dead.
- **JSON export** and the **`export` command**, from spec section 6.5's data
  ownership note.

## Next

ON Notes is the next project: its own spec, its own plan, and — if the platform
is right — no platform changes at all.
