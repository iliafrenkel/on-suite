# Disable-apps flag design

Status: approved 2026-09-16. Small, platform-level addition: a startup
flag/env var that turns off one or more registered apps for a given
deployment, without touching their schema or data.

## 1. Motivation

ON Suite is one binary with a growing number of apps (`notes`, `paste`,
`reader`, more to come). Someone running the suite may only want a subset —
e.g. just ON Reader, none of the others cluttering the nav. Today the only
way to do that is to fork `registeredApps()` in
[cmd/onsuite/main.go](../../../cmd/onsuite/main.go) and rebuild. This adds a
supported way to do it at deploy time, no rebuild required.

## 2. Decisions

| # | Item | Choice |
|---|---|---|
| 1 | When it can change | Startup only — a flag/env var, read once at process start. No runtime/admin-UI toggle. |
| 2 | How apps are named | Deny-list: `-disable-apps` lists the apps to turn *off*. Everything not named stays on, so a newly added app is on by default. |
| 3 | What happens to a disabled app's schema/data | Nothing is dropped. Its tables keep whatever data they already had; its migrations simply stop running while it's disabled. Re-enabling it lets normal migration catch-up bring it up to date. |
| 4 | Visibility while disabled | Fully invisible: no nav entry, no routes (404, same as any unknown app path), no admin stats, no `onsuite export` output, no scheduled jobs. Only the untouched DB tables remain, in case it's re-enabled later. |

## 3. Config

New setting in [internal/platform/config/config.go](../../../internal/platform/config/config.go),
following the existing pattern (`settingSpecs` entry, `fs.StringVar`, admin-page
visibility):

- Flag: `-disable-apps` (string, comma-separated app IDs, e.g. `paste,reader`)
- Env: `ONSUITE_DISABLE_APPS`
- Default: `""` (nothing disabled)
- `Config` gains `DisabledApps []string` — parsed from the raw flag value by
  splitting on `,`, trimming whitespace, and dropping empty entries. No
  further validation happens in `config`: it has no knowledge of which app
  IDs actually exist (the platform/app boundary rule already forbids
  `config` reaching up to `app`), so unknown-ID and empty-set checks happen
  where `registeredApps()` lives instead (§4).
- `Setting.Value` is the joined, cleaned list (or `""`), same as any other
  optional string setting like `tls-domain` — no special "(none)" formatting.

## 4. Filtering

[cmd/onsuite/database.go](../../../cmd/onsuite/database.go) is the one place
every command (`serve`, `export`, `user`, `backup`) builds the `app.Registry`,
via `app.NewRegistry(registeredApps()...)`. That single call site is where
filtering happens:

```go
// filterApps returns the apps that should be part of this run's registry:
// all registered apps minus the ones named in disabled. It fails on an
// unknown app ID (almost certainly a typo in -disable-apps/ONSUITE_DISABLE_APPS)
// and on a result that would leave no apps at all, since a binary with zero
// apps is never an intentional configuration.
func filterApps(all []app.App, disabled []string) ([]app.App, error)
```

Added next to `registeredApps()` in
[cmd/onsuite/main.go](../../../cmd/onsuite/main.go). `database.go`'s call
becomes:

```go
apps, err := filterApps(registeredApps(), cfg.DisabledApps)
if err != nil {
    return nil, nil, 0, err
}
registry, err := app.NewRegistry(apps...)
```

Validation:
- Each entry in `disabled` must match some `all[i].Meta().ID`; an unknown ID
  is an error naming the bad value and listing the known IDs.
- A duplicate entry in `disabled` is harmless (treated as a set) and not an
  error — typing the same ID twice isn't a meaningful mistake the way a typo
  is.
- If filtering would leave `apps` empty, that's an error too — it always
  means a misconfigured `-disable-apps`, never an intentional "run with no
  apps" mode.

No other code changes anywhere: `app.Registry` (nav, routes, migrations,
`Export`, `Stats`, `RegisterJobs`) already derives everything purely from the
apps it was constructed with, per
[internal/platform/app/app.go](../../../internal/platform/app/app.go). A
disabled app is simply never in that list for the duration it's disabled, so:

- **Nav/dashboard**: `Registry.NavItems()` and the home page's app list omit
  it. It is *not* added to `comingSoonApps`
  ([cmd/onsuite/stack.go](../../../cmd/onsuite/stack.go)) — that list is for
  apps that don't exist yet, a different concept from one that exists but is
  turned off — so a disabled app leaves no trace on the dashboard at all.
- **Routes**: never mounted, so its paths 404 like any nonexistent route.
- **Migrations**: `Registry.Migrations()` only walks its own `apps`, so a
  disabled app's `.sql` files are skipped. Existing tables and rows are
  untouched (migrations are additive/never destructive already, so "stop
  applying new ones" cannot corrupt what's there).
- **Export/Stats/Jobs**: `Registry.Export`/`Stats`/`RegisterJobs` only see
  registered apps, so a disabled app is silently absent from all three — the
  same "skipped, not failed" behavior these already use for apps that don't
  implement the optional `Exporter`/`Stater`/`Scheduler` interfaces.

## 5. Re-enabling

Re-enabling an app is just removing its ID from `-disable-apps`/
`ONSUITE_DISABLE_APPS` and restarting. Its tables are already there;
`Registry.Migrations()` sees it again and applies whatever migrations
accumulated while it was off, the same catch-up `db.Apply` already performs
for any migration that hasn't run yet. No special-cased "resume" logic is
needed.

## 6. Testing

- `config.Parse`: `-disable-apps` parses a comma-separated list, trims
  whitespace, drops empty entries, and appears correctly in `Settings()`
  (value, default, source-is-flag/env/default) alongside the existing
  setting tests.
- `filterApps`: disabling a known app removes exactly it and keeps the rest
  in order; disabling an unknown ID errors and names the bad value; disabling
  every registered app errors; a duplicate ID in the list is not an error and
  behaves like naming it once.
- One `cmd/onsuite` integration-level test (alongside the existing
  `database_test.go`/`backup_test.go` style) building a registry with an app
  disabled, confirming: its routes 404, `Registry.NavItems()` omits it,
  `Registry.Migrations()` doesn't include its migrations, and (using a
  fixture with two apps, one disabled) `Registry.Export`/`Stats` omit it.
- No changes needed to `internal/arch/arch_test.go` — this doesn't add any
  new import edges between packages.
