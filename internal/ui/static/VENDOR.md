# Vendored front-end files

These are committed rather than fetched at build or run time: `onsuite` must
serve a working page on a host with no outbound internet access.

To update one, download it, verify the checksum against the project's own
published release, update the table, and commit the new file in its own commit
so the diff is reviewable.

The fonts are Google Fonts' per-script `.woff2` subsets (Latin, Cyrillic,
Cyrillic Extended), not the upstream release files, so their checksums
match what Google Fonts serves, not IBM's or Adobe's release archives. The
Version column is the font's own version string (its `name` table, ID 5),
which you can read with `fontTools`: `ttx -t name -o - <file>.woff2`.

| File | Version | SHA-256 | Source |
|---|---|---|---|
| `htmx.min.js` | 2.0.10 | `71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de` | https://unpkg.com/htmx.org@2/dist/htmx.min.js |
| `fonts/ibm-plex-mono-400-cyrillic.woff2` | 2.3 | `1431edf60f08a3d3c88c83a2919cfce1b53149939375adb2ac4f587efeed80af` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-400-cyrillic-ext.woff2` | 2.3 | `f8c22ec1804bd6966b70f43ac7c327025645b4c09597117fca2a5a6c750106fe` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-400-latin.woff2` | 2.3 | `7ced53420784c93cecccb7c27237594f0e41e5a7990581565f0aef8eda9e9d2f` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-500-cyrillic.woff2` | 2.3 | `ef2d8c602770429638da4674c489da3375542ec5c9b856d0623060025831b59c` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-500-cyrillic-ext.woff2` | 2.3 | `3febe5c7f22e4b07b4552738673c3dbe8bced2dc5f48b866edf7ccd9552d109a` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-500-latin.woff2` | 2.3 | `6e618344335cc436ffd25deba4f4cb5a60ca6c3a4832e67f236ca43922f74eea` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-600-cyrillic.woff2` | 2.3 | `8789b11d9afc3f686c023ed7ff411f7f3bc838be0b0933685cfb278313cf7e78` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-600-cyrillic-ext.woff2` | 2.3 | `83109cdd4f8665b475dfc11d61bd44da328bf7e6eab3d3953d39c49b1b9f6015` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-mono-600-latin.woff2` | 2.3 | `cecb8f0bc58b996a828bc0a23e2814e4acec33f0f531adf565c0248e242061e1` | https://fonts.google.com/specimen/IBM+Plex+Mono (IBM Plex Mono, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-sans-cyrillic.woff2` | 3.201 | `9409ee21811f0f5272b7e1d828fe8838005b0573481ef906e7e55b63f6610ab0` | https://fonts.google.com/specimen/IBM+Plex+Sans (IBM Plex Sans variable, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-sans-cyrillic-ext.woff2` | 3.201 | `7d0037a170882a49e65a45c38ef195237bb07b1cc791d078379f0ac0d7ae1935` | https://fonts.google.com/specimen/IBM+Plex+Sans (IBM Plex Sans variable, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-sans-latin.woff2` | 3.201 | `e2291e842cf5af167122a22881a740c7f2dda7716f1e8cd76680264f4a859470` | https://fonts.google.com/specimen/IBM+Plex+Sans (IBM Plex Sans variable, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400-cyrillic.woff2` | 2.6 | `1b706597c0169c04847649fe445491c73d674bdbfcf82b258d0ef1b59ade5f0d` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400-cyrillic-ext.woff2` | 2.6 | `6737de90a1acf5779c59625cf9f29aa8d31e969d8e2b1162926ef35d32e0bf03` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400-latin.woff2` | 2.6 | `cb2c5eee2c0a43ff30d2365407c7bc8b20e3bd90720a4a64102ba0b328022a02` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400i-cyrillic.woff2` | 2.6 | `8fcac7ce093cbed5b49219d6fdd36ec79e56c3e846d6884ecc4f6b6c96d1eaea` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif Italic, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400i-cyrillic-ext.woff2` | 2.6 | `ae15cfd77c01a64fba7ba8a7d103a9af142260acf70e69cc61673dcef98b2099` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif Italic, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-400i-latin.woff2` | 2.6 | `d3f861b1ca55e50ca94de4c1756cb0d67c73de615172adf6b5b2078d939c723a` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif Italic, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-500-cyrillic.woff2` | 2.6 | `c3be9daf8725a1a22d486aa5f0a78f662b1ef46d32f894e157d4ffe32e96d962` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-500-cyrillic-ext.woff2` | 2.6 | `10a376000794baef64cd9ed7f368fc26e7baac93f308198f6d0c1fad659aaf81` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-500-latin.woff2` | 2.6 | `677082fc2e9ee60701b874fe5983c98ea9fb3b588cc1b02058ff9bf29ea783e4` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-600-cyrillic.woff2` | 2.6 | `19e51b19bd700917b553ab861cc5a4d48362e090e9a0f538625428312f6f1853` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-600-cyrillic-ext.woff2` | 2.6 | `1ca6b912bb9b69cf4dac6cbf528382575d959655cc577b25f63f4198b3e88962` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/ibm-plex-serif-600-latin.woff2` | 2.6 | `e279e4f8baa0d4634d98868092f2198ae0dcd9e142c935822c385b9843352a9b` | https://fonts.google.com/specimen/IBM+Plex+Serif (IBM Plex Serif, Google Fonts subset, SIL OFL 1.1) |
| `fonts/source-serif-4.woff2` | 4.004 | `c1df4596be5029233ed2afbb8b2f6ea20784b3fb1aa5d6b5c6519ccd85eb3dfb` | https://fonts.google.com/specimen/Source+Serif+4 (Source Serif 4, Google Fonts subset, SIL OFL 1.1) |
| `fonts/source-serif-4-cyrillic.woff2` | 4.004 | `25e85aef8acff54f91ea7e85c9c2c260caf97101fdef194ed6b8b58bc34ce3eb` | https://fonts.google.com/specimen/Source+Serif+4 (Source Serif 4, Google Fonts subset, SIL OFL 1.1) |
| `fonts/source-serif-4-cyrillic-ext.woff2` | 4.004 | `f423aab1098e5b0dae59d200aadbb9433e687f505425c7e73d1f65ae2b59016e` | https://fonts.google.com/specimen/Source+Serif+4 (Source Serif 4, Google Fonts subset, SIL OFL 1.1) |
