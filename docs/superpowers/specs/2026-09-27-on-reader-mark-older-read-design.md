# ON Reader — Mark older than X as read (issue #307)

## Goal

Next to "Mark all read", let the reader mark only items **older than 1 day**
or **older than 1 week** as read, via a split button:

```
+---------------+---+
| Mark all read | ⌄ |
+---------------+---+
| Older than 1 day  |
+-------------------+
| Older than 1 week |
+-------------------+
```

## Semantics

- **Age is measured on `published_at`** — the date the list shows and sorts
  by — so "older than a day" matches what is on screen. Publisher dates can
  be wrong; that is accepted.
- **Rolling window from now:** "1 day" = `published_at < now − 24h`,
  "1 week" = `published_at < now − 168h`. Not calendar days. An item exactly
  at the cutoff stays unread (strict `<`).
- **Same scope as Mark all read:** the list being viewed (a feed, All, or
  Starred), including the existing `fetched_at >= sub.added_at` visibility
  cutoff. The search box is ignored, as it is today.
- **No confirmation**, matching the current button.

## Design

One endpoint with an optional cutoff (chosen over separate endpoints, which
would only duplicate routing, and over a client-supplied timestamp, which
would trust client clocks and need JavaScript).

### Store

`MarkAllRead(ctx, userID, scope, subID, now, olderThan time.Duration)`.
`olderThan == 0` keeps today's behaviour; a positive value appends
`AND i.published_at < ?` with `formatTime(now.Add(-olderThan))`. Callers are
updated to pass `0` where they mean "everything".

### Handler

`POST /reader/read-all` reads an optional `older_than` form field against a
closed allow-list:

| value   | cutoff |
|---------|--------|
| (empty) | none — mark everything |
| `day`   | 24h    |
| `week`  | 168h   |

Anything else → 400 via `a.deps.Errors.Status`. The re-render is unchanged
(`renderIndex` on the form's list context).

### Template / CSS

The `.reader-mark-all` form becomes a split button: the existing "Mark all
read" submit on the left, and on the right a `<details class="outline-menu">`
chevron (the same no-JS disclosure used for folder/row menus, see
PATTERNS.md) whose list holds two submit buttons inside the **same form**:
`name="older_than" value="day"` / `value="week"`. htmx includes the
submitter's name/value, and with JavaScript off the buttons submit the form
normally. The panes re-render on success, which closes the menu.

CSS joins the two halves visually (shared border, no gap) and right-aligns
the menu under the chevron. The chevron has `aria-label="More mark-read
options"`.

## Testing

- Store: for feed, All and Starred scopes, `day`/`week` cutoffs mark only
  items published before the cutoff; items at or after it stay unread;
  `olderThan == 0` still marks everything.
- Handler: `older_than=day` marks only old items end to end; an unknown
  value returns 400 and changes nothing.
- Template: the list toolbar renders both menu options with the right
  `older_than` values.
