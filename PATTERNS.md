# Patterns

An index of recurring, deliberate patterns in this codebase — grep here
before inventing a new way to solve a problem this project has already
solved. Each entry is a pointer, not an explanation: the canonical example's
own comments carry the "why." When you copy a pattern, cross-reference the
original the way existing copies do (e.g. "mirrors X's own Y") rather than
extracting a shared abstraction — see "Cross-app mirroring" below for why
that's often the deliberate choice here, not an oversight.

Add to this list when you notice yourself reusing (or wishing you could
find) a shape that already exists elsewhere. Keep entries to one line each;
if an entry needs more than that to explain, the explanation belongs in the
canonical example's own comment, not here.

- **Server-rejection surfaced as a dismissable notice** — reach for this
  when an htmx request can be rejected (4xx/5xx) and swap:false would
  otherwise leave the user with no feedback at all: listen for
  `htmx:responseError`, insert a `.notice.notice-error` element, clear it on
  the next successful swap of the same target. Canonical:
  `internal/apps/notes/static/notes.js`'s `initPasteErrors` (copied by
  `initMoveErrors` in the same file).

- **View-model projection instead of embedding a domain struct** — reach for
  this whenever a struct is rendered somewhere less trusted than where it
  was loaded (a public page, an export, an API response): declare only the
  fields that destination actually uses, rather than embedding the full
  domain type and hoping nothing sensitive gets rendered. Canonical:
  `internal/apps/notes/share.go`'s `sharedRow`/`sharedView`.

- **Reflection-based structural invariant test** — reach for this to pin an
  invariant like "this type must never carry field X," which a
  template-only test can't catch if the template happens not to print that
  field today. Canonical: `internal/apps/notes/view_test.go`'s
  `TestSharedRowHasNoPrivateFields`.

- **Mobile/touch CSS added to the existing media-query block** — reach for
  this for a responsive or touch-affordance fix: extend the existing
  `@media (max-width: 640px)` or `@media (hover: none)` block in
  `internal/ui/static/app.css` rather than opening a new one elsewhere in
  the file.

- **Chrome visibility gated on `Shell.LoggedIn`** — reach for this whenever
  a piece of shared chrome (nav, breadcrumb, user menu) must not leak a
  login-gated affordance to a signed-out visitor on a public page.
  Canonical: `internal/ui/templates/base.html`'s breadcrumb link.

- **`Ops` write-transaction wrapper** — reach for this whenever a mutation
  touches more than one row/step and must not half-apply; add a method to
  the store's `Ops` type, run only inside `Store.Do`, and never call
  another `Store` method from inside that closure. Canonical:
  `internal/apps/notes/tree.go`'s `Ops` type and `Store.Do`.

- **Real-SQLite-file store tests, not `:memory:`** — reach for this for any
  store/SQL test: `:memory:` can hide bugs that only show up with real
  file-backed transactions and WAL behavior. Canonical:
  `internal/apps/notes/store_test.go`'s test fixture setup.

- **Status-code-to-title/message table for error pages** — reach for this
  when adding a new HTTP error response: add an entry to `titles` rather
  than hand-writing another error page. Canonical:
  `internal/platform/web/errors.go`'s `titles` map and `Errors.Status`.

- **`hx-post` mirrors `formaction` (progressive enhancement)** — reach for
  this for any structural button: give it a real `formaction` first, then
  make `hx-post` identical, so a JS-disabled request and an HTMX request hit
  the same route. Enforced by
  `internal/apps/notes/handlers_test.go`'s
  `TestEveryStructuralButtonMirrorsItsFormactionAsHTMX`.

- **`hx-swap-oob` for state living outside the swapped fragment** — reach
  for this when a UI element (a toolbar toggle, a badge) sits outside the
  container a normal HTMX response replaces, so it needs an out-of-band
  copy to stay in sync. Canonical: `internal/apps/notes/view.go`'s `OOB`
  field, used by outline.html's toolbar toggle.

- **Cross-app mirroring instead of a shared package** — reach for this when
  two apps need identical behavior (slug generation, revoke-then-remint
  sharing) but apps must never import each other (see AGENTS.md's
  Architecture section): duplicate the logic and say so in the doc comment
  ("mirrors X's own Y... apps never import each other, so this is an
  independent implementation") rather than extracting a shared package.
  Canonical: `internal/apps/notes/share.go`'s `newShareSlug`, which mirrors
  `internal/apps/paste/store.go`'s.

- **Owner-matching re-checked at every tree-descent step** — reach for this
  whenever code walks a parent/child structure keyed by a plain foreign key
  rather than a composite `(parent_id, user_id)` one: re-verify `user_id`
  at each level of the walk, not just once at the root. Canonical:
  `internal/apps/notes/store.go`'s `Outline`, mirrored by `export.go` and
  `share.go`'s `SharedSubtree`.

- **Cross a layering boundary with an injected func, not an import** —
  reach for this when a lower layer needs a value or behavior only a
  forbidden-to-import higher layer knows (a constant, a URL scheme): add a
  field to that layer's `Options`/config struct and wire it from whichever
  caller may import both, the same way `render.Options.AssetURL` and
  `CSRFFieldName` (a `template.FuncMap` entry, since it's exposed to
  templates) let `render` use `web`'s asset URLs and CSRF field name
  without ever importing `web`. Canonical: `internal/platform/render/render.go`.

- **App boundary via `app.App` + `Deps`, no cross-app imports** — reach for
  this when wiring in a new app: implement `app.App`, take only `app.Deps`,
  never import another app package. Enforced by
  `internal/arch/arch_test.go`. Canonical:
  `internal/platform/app/app.go`.

- **Doc comments state "why," with a spec section or issue number** — reach
  for this convention on any nontrivial function: cite the spec section
  (`docs/superpowers/specs/...`) or GitHub issue that justifies a
  non-obvious choice, not just a restatement of what the code does.
  Canonical: pervasive — e.g. `internal/apps/notes/tree.go`'s `Ops` doc
  comment, `internal/apps/notes/store.go`'s `Outline`.
