# Backlog: follow-ups from Plan 3's final review

Source: the final whole-branch code review of Plan 3 ("ON Paste and
Operations"), run 2026-08-19 after all nine tasks (15-23) were implemented and
individually reviewed. The review found 5 Important and 12 Minor issues. Of
the 5 Important issues, all traced back to something the plan itself
literally specified rather than an implementer mistake; three were fixed with
explicit approval (session-sweep-on-backup, Docker `/data` ownership plus
upgrade-doc ordering, and lowering `paste.MaxBodyBytes`) and are already on
`main`. The remaining one Important item and all 12 Minor items below were
left as deliberate follow-ups rather than fixed inline.

File:line references are as of the review's commit and may have drifted since
— treat them as a starting point for relocating each issue, not a guarantee.

**2026-08-20:** each of the 12 Minor findings below was filed as a GitHub
issue (#7–#18) so it can actually be closed instead of relying on someone
remembering to delete a line here. This file is kept as the narrative
record of the review; the issues are the actionable tracker going forward.
The Declined item below was intentionally not filed, since it's a decision
already made rather than an open task.

## Declined (deliberate, not a gap)

**"Public surface is exactly 3 routes" test only checks a hardcoded path
list, not a true count.**
`internal/apps/paste/handlers_test.go` (`TestPublicSurfaceIsExactlyThreeRoutes`)
asserts that 3 named paths are reachable and 4 named paths are not. It would
catch *promoting* one of those specific 4 paths to public, but a hypothetical
new `r.PublicFunc(...)` registration for some other path would pass unnoticed.
The plan's own text explicitly chose this behavioral test over an exact
`count(public routes) == 3` assertion, reasoning that the latter "would need
an accessor added for a test's sake." Fixing this properly means adding a
`Registry.Routes()`-style accessor to `internal/platform/app`, which is a
small platform API surface addition and a reversal of that deliberate choice.
**Explicitly declined for now** — revisit if a future app's route surface
becomes a higher-stakes security boundary than ON Paste's.

## Minor findings

1. **Paste's own HTTP test harness skips several platform middlewares.**
   _(Filed as [#7](https://github.com/iliafrenkel/on-suite/issues/7).)_
   `internal/apps/paste/handlers_test.go`'s `newServer` builds `csrf`, `authn`
   and `web.Chain` by hand but omits `SecurityHeaders`, `LimitBody`,
   `Recover`, and `RequestLog` — the ones `cmd/onsuite/stack.go` wires into
   the real server. This means CSP/`nosniff` and the platform's global body
   limit are never exercised by ON Paste's own tests (it's part of why the
   `MaxBodyBytes`/CSRF-403 collision fixed in this plan wasn't caught by the
   existing suite). Fix: share the real stack-construction helper between
   `cmd/onsuite` and the paste test harness instead of reimplementing it.

2. **An interrupted snapshot can leave a partial file that looks like a good
   backup.**
   _(Filed as [#8](https://github.com/iliafrenkel/on-suite/issues/8).)_
   `cmd/onsuite/serve.go` cancels the maintenance context on
   shutdown without waiting for an in-flight pass to finish, and
   `internal/platform/db`'s `BackupTo` doesn't remove the destination file if
   `VACUUM INTO` fails partway. A snapshot interrupted by `SIGTERM` can leave
   `backups/onsuite-<timestamp>.db` with a legitimate-looking name, which
   `pruneSnapshots` will happily count and which the deploy guide's restore
   procedure would happily use. Fix: `os.Remove(dest)` on the `BackupTo`
   error path, and optionally have shutdown wait for an in-flight maintenance
   pass via a `sync.WaitGroup`.

3. **Misleading log line when only pruning fails, not the snapshot.**
   _(Filed as [#9](https://github.com/iliafrenkel/on-suite/issues/9).)_
   `cmd/onsuite/backup.go`'s `maintain()` logs `"snapshot failed"` whenever
   `writeSnapshot` returns a non-nil error — but that error is also returned
   when the snapshot itself succeeded and only the subsequent pruning step
   failed. An operator reading logs would wrongly conclude no backup exists.
   Fix: distinguish the two cases in the log message (the underlying error
   from `writeSnapshot` already says "wrote %s but pruning failed" — surface
   that distinction in the log call too).

4. **Dead assignment.**
   _(Filed as [#10](https://github.com/iliafrenkel/on-suite/issues/10).)_
   `internal/apps/paste/handlers.go`'s `share` handler
   does `_ = slug` after calling `a.store.Share(...)`. Simplify to
   `if _, err := a.store.Share(...); err != nil { ... }` and drop the unused
   variable.

5. **`onsuite backup` silently ignores stray positional arguments.**
   _(Filed as [#11](https://github.com/iliafrenkel/on-suite/issues/11).)_
   `cmd/onsuite/backup.go`'s `backupCmd` discards `parseInterspersed`'s
   returned positional-args slice, so `onsuite backup /srv/out.db` (a likely
   typo for `--out /srv/out.db`) writes to the default location and reports
   success rather than an error. `exportCmd` already validates its positional
   count; `backupCmd` should reject any stray positional argument the same
   way.

6. **Comment/code drift on the raw-text `Content-Disposition` header.**
   _(Filed as [#12](https://github.com/iliafrenkel/on-suite/issues/12).)_
   `internal/apps/paste/handlers.go`'s `writeRaw` comment says
   `Content-Disposition` "names the download without forcing one," but the
   code sets a bare `Content-Disposition: inline` with no `filename=`
   parameter — nothing is actually named. Either add a `filename=` parameter
   (e.g. derived from the snippet's title or id) or correct the comment.

7. **The shipped systemd unit doesn't pass `--secure-cookies`.**
   _(Filed as [#13](https://github.com/iliafrenkel/on-suite/issues/13).)_
   `docs/deploy/onsuite.service` is written for the reverse-proxy deployment
   story, and `docs/deploy/README.md` correctly says to pass
   `--secure-cookies` behind a proxy — but the unit file people actually copy
   doesn't include it on `ExecStart`. Add it (with a comment noting it's
   unnecessary for the built-in-TLS variant of the unit).

8. **No cache or robots directives on the shared snippet page.**
   _(Filed as [#14](https://github.com/iliafrenkel/on-suite/issues/14).)_
   `viewShared` (`internal/apps/paste/handlers.go`) sets neither
   `Cache-Control` nor `X-Robots-Tag: noindex`, while the raw-text sibling
   `writeRaw` does set `Cache-Control: no-store`. A share slug is a revocable
   credential; without these headers, a shared page can persist in an
   intermediary cache or get indexed by a crawler even after the link is
   revoked. Cheap hardening, and it makes the two shared-page endpoints
   consistent with each other.

9. **The share link is rendered as a bare, non-copyable path.**
   _(Filed as [#15](https://github.com/iliafrenkel/on-suite/issues/15).)_
   `internal/apps/paste/templates/view.html` displays `/paste/s/<slug>`
   inside a `<code>` block rather than as an absolute, clickable/copyable
   link. The app deliberately doesn't know its own origin today, so this may
   have been intentional — worth a deliberate decision (e.g. deriving the
   origin from the request, or leaving as-is with a "copy the full URL"
   note) rather than leaving it as an accident.

10. **Loose boolean parsing for a security-relevant flag.**
    _(Filed as [#16](https://github.com/iliafrenkel/on-suite/issues/16).)_
    `internal/platform/config/config.go` only recognises the literal string
    `"true"` for `ONSUITE_SECURE_COOKIES`; values like `"1"`, `"yes"`, or
    `"TRUE"` silently do nothing (cookies stay non-`Secure`), with no warning
    logged. `envDuration`/`envInt` have the same silent-fallback-on-typo
    behavior. Since this specific flag is security-relevant, consider
    `strconv.ParseBool` (which accepts `1/t/T/TRUE/true/True` etc. and
    returns an error you can log) at least for `ONSUITE_SECURE_COOKIES`.

11. **CI pins no staticcheck version.**
    _(Filed as [#17](https://github.com/iliafrenkel/on-suite/issues/17).)_
    `.github/workflows/ci.yml` runs
    `go run honnef.co/go/tools/cmd/staticcheck@latest`. A new staticcheck
    release that doesn't yet understand a newer `go.mod` Go directive (as
    happened historically) can turn CI red with no code change on this
    project's side. Pinning to a specific version and bumping it
    deliberately is the standard trade-off here, and doesn't add a module
    dependency since it's invoked via `go run`, not imported.

12. **The plan's "ships as a signed release" claim isn't backed by actual
    signing.**
    _(Filed as [#18](https://github.com/iliafrenkel/on-suite/issues/18).)_
    `.goreleaser.yaml` produces `checksums.txt` (integrity) but has
    no `signs:` block (authenticity) — cosign or GPG signing was never added.
    This doesn't fail the plan's actual Definition of Done (which doesn't
    require signing), so it's a plan-wording issue more than a missed
    requirement: either add a signing step or adjust the project's stated
    claims to match what's actually shipped.
