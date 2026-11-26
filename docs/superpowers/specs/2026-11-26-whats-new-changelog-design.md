# What's new: in-app changelog and badge

Date: 2026-11-26. Status: approved design (small change).

## Why

Release notes exist only on GitHub release pages, as the tag annotation
plus a commit list. People using a self-hosted ON Suite have no way to know
their server was upgraded or what changed. They should notice an update and
be able to read about it inside the app.

## Goals

- A changelog page inside the app, built into the binary, readable without
  internet access.
- A "What's new" badge that each user sees once after their server is
  upgraded to a new version, and that clears once they read the changelog.
- The footer version links to the changelog.
- One source for the release text, so the app and GitHub always say the same thing.

## Non-goals

- Telling the admin that a newer release exists upstream. That needs the
  server to contact GitHub. It's filed as a separate follow-up issue.
- Showing the generated commit list in the app. That stays on GitHub.
- Popups, modals or toasts. The badge is quiet.

## Approaches

**Changelog source**
- **A (picked): `docs/user/changelog.md` in the repo.** Newest release first.
  Embedded with the other user guides and served at `/help/changelog`.
  Release tags copy their name and summary from the top entry.
- B: generate the changelog from git tags at build time. Rejected: the
  notes only exist at tag time, local builds have none, and the text can't
  be reviewed in a PR.

**Where "seen" is stored**
- **A (picked): a `seen_version` column on `users`.** It's the same on every device.
- B: a cookie, like the sidebar's collapsed state. Rejected: every browser
  and phone would show the badge again.

## Design

**Changelog file.** `docs/user/changelog.md` starts with `# What's new`.
Each release is one section:

```markdown
## 6.4.0: ON Suite - Your assistant reads with you

*25 November 2026*

Assistants can now work with ON Reader and ON Later: ...
```

Past releases are filled in from the existing tag annotations: the subject
is the title and the body is the summary. Tags whose message is only the
version get a heading and the date, plus a one-line summary we write from
that release's PR titles.

**Help integration.** `changelog` is added to the help page list
(`internal/platform/help/pages.go` `order`), labelled "What's new" and
placed after Administration. It's public like every help page.

**Badge state.**
- Migration `0003` adds `users.seen_version TEXT NOT NULL DEFAULT ''`.
  Existing users get `''`, so they see the badge once after this ships.
- New accounts (sign-up, admin create, CLI) are created with
  `seen_version` set to the running version, so nobody is badged on first login.
- The shell gets `Shell.WhatsNew bool`. It's true when the user is logged
  in, the version isn't `dev`, and `seen_version != Version`. That's a plain
  inequality, not a semver comparison; a downgrade showing the badge is fine.
- When a logged-in user opens `GET /help/changelog`, their `seen_version`
  is set to the running version, and the page renders without the badge.
  *Builder's call:* this is a write on GET. It's harmless, since the
  worst a forged request can do is clear a badge, and it keeps the page
  working with JavaScript off.

**UI.**
- A small accent dot on the user-menu summary button when `WhatsNew` is
  true. It has an accessible label, for example "What's new" in a
  visually-hidden span.
- A new "What's new" item in the user menu, after Help, linking to
  `/help/changelog`. It also carries the dot when `WhatsNew` is true. The
  item is always there, badge or not.
- The footer version (`ON Suite · 6.4.0`) becomes a link to `/help/changelog`.
  `dev` builds stay plain text.

**Release routine.**
- `docs/developers/releasing.md` gains a step before tagging: add the new
  entry to `changelog.md` in a PR and merge it.
- A small `scripts/tag-message.sh` prints the top entry as a tag message
  (`title`, blank line, summary), so the tag is cut with
  `git tag -a vX.Y.Z -F <(scripts/tag-message.sh)`. The script fails if the
  top entry's version doesn't match the version it's given.

**Tests.**
- The changelog file parses: every `##` heading matches `N.N.N: title`,
  versions are strictly descending, and there are no duplicates.
- Badge logic: dev, logged out, seen, and unseen.
- Opening the changelog clears the badge.
- New users start as seen.
- The footer links to the changelog.
- The migration runs.

## Left to the builder

- Exact dot styling, within the existing tokens: accent colour, and fine
  in both palettes and both themes.
- Where the "seen" read and write live: the auth/users store, with the
  help handler calling it.
- The wording of one-line summaries for bare-version tags.
