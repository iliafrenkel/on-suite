# Vendored front-end files

These are committed rather than fetched at build or run time: `onsuite` must
serve a working page on a host with no outbound internet access.

To update one, download it, verify the checksum against the project's own
published release, update the table, and commit the new file in its own commit
so the diff is reviewable.

| File | Version | SHA-256 | Source |
|---|---|---|---|
| `htmx.min.js` | 2.0.10 | `71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de` | https://unpkg.com/htmx.org@2/dist/htmx.min.js |
