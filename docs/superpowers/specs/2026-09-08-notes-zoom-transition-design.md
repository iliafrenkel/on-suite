# ON Notes: cross-fade transition on zoom in/out

## Problem

Zooming into a note (making it the new root, `/notes/{id}`) and zooming back
out via the breadcrumb are both instant, hard cuts — a plain `<a href>` full
page navigation. There's no visual continuity between "where I was" and
"where I am now", and no shared transition infrastructure exists yet for
Notes, Paste, or future apps (Flash, Reader) to build on.

## Goals

- Zooming into a note and zooming back out (breadcrumb "up") get a smooth
  cross-fade instead of a hard cut or full page reload.
- Browser back/forward navigation between zoom levels also animates, so the
  effect is consistent regardless of how the user navigates.
- The transition degrades safely: browsers without the View Transitions API
  get the current instant behavior, not a broken page.
- `prefers-reduced-motion: reduce` disables the animation.
- The approach is a reusable pattern (named region + `hx-swap` modifier) that
  Paste, Flash, and Reader can adopt later without new dependencies.

## Non-goals (explicitly deferred)

- Any shared-element/morph effect (row growing into the detail view). This is
  a plain cross-fade only.
- Animating any other `#outline`-mutating action (indent, outdent, done,
  archive, delete, move, new). These stay instant — they're frequent, small
  edits where a fade would read as lag.
- A JS animation library or custom transition engine. This is CSS + htmx's
  built-in View Transitions integration only.
- Changes to Paste, Flash, or Reader themselves — this spec only establishes
  the reusable pattern in the shared stylesheet, it doesn't apply it
  elsewhere.

## Design

### CSS (`internal/ui/static/app.css`)

Give the `#outline` container `view-transition-name: outline;`. Add:

```css
::view-transition-old(outline),
::view-transition-new(outline) {
  animation-duration: 0.18s;
  animation-timing-function: ease;
}
```

This overrides the browser's default combined fade+scale/cross-fade-with-clip
animation, producing a pure opacity cross-fade instead. Wrap the whole block
in `@media (prefers-reduced-motion: reduce) { animation-duration: 0.001s; }`
(or equivalent override) so reduced-motion users get an effectively instant
swap without a separate code path. A short comment above the block documents
this as the reusable "named-region cross-fade" pattern for other apps.

### HTMX wiring (`internal/apps/notes/templates/outline.html`)

The two existing plain zoom links become htmx-driven:

- Row zoom link (~line 110): the target note's own link.
- Breadcrumb "up" links (~line 329): ancestor links in the breadcrumb trail.

Both change from `<a href="/notes/{{.ID}}">` to:

```html
<a href="/notes/{{.ID}}"
   hx-get="/notes/{{.ID}}"
   hx-target="#outline"
   hx-swap="innerHTML transition:true"
   hx-push-url="true">
```

The `href` is kept (not removed) so the link still works with JS disabled or
htmx unavailable — progressive enhancement, consistent with how this codebase
already treats `?q=` filtering as a real query parameter rather than a
client-only feature.

No server-side changes are needed: `renderOutlineOrFragment`
(`internal/apps/notes/handlers.go:86`) already returns just the `#outline`
fragment for `hx-request`s and the full page otherwise, and already
special-cases htmx history-restore requests
(`web.IsHTMXHistoryRestore`, `handlers.go:87`) to avoid the fragment bug
fixed in #201. This spec relies on that existing branching as-is.

Only these two link sites get `transition:true`. Every other `hx-swap` in
`outline.html` (indent, outdent, done, archive, delete, move, new) is
unchanged.

### Back/forward navigation

`hx-push-url="true"` puts a real URL in browser history for each zoom level,
so back/forward re-enters htmx's navigation path rather than doing a plain
browser reload.

There's one open question this spec does not resolve in advance: whether
htmx's own history-snapshot cache (its `restoreHistory` path, which can
restore from a local snapshot without a server round-trip) honors the
`transition:true` swap modifier the same way a live request does. This will
be verified empirically during implementation by testing back/forward in the
browser. If the snapshot-restore path does not animate, that's an acceptable
degrade (instant back/forward, same as pre-change behavior) — not a defect
to work around with additional code.

### Browser support / fallback

htmx's `transition:true` modifier internally checks for
`document.startViewTransition` and falls back to a normal (instant) swap when
unsupported. No feature-detection code is needed in this codebase; degrade is
automatic and free.

### Reusability for Paste/Flash/Reader

The pattern established here is generic and requires no shared JS or new
dependency: give the swapped container a `view-transition-name`, override its
`::view-transition-old/new` rule for the desired effect (cross-fade, or
something else later), and add `transition:true` to the specific `hx-swap`
attributes that should animate. The comment left in `app.css` documents this
so it's discoverable when Flash/Reader are built.

## Testing

- **Manual, via browser tooling:** zoom into a note (row click), confirm
  cross-fade; zoom back out via breadcrumb, confirm cross-fade; use browser
  back then forward across at least two zoom levels, confirm behavior
  (animated or safely instant, per the open question above); confirm
  indent/done/archive/delete/move/new remain instant with no visual
  regression; toggle OS/browser `prefers-reduced-motion` and confirm the
  transition is effectively disabled.
- **No automated visual-transition tests** — not worth the complexity for a
  CSS-only effect layered on already-tested fragment-rendering logic.
