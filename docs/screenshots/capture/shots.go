package main

// readerScrollToActive scrolls the article list so the open article sits
// near the top, two rows down, instead of cut off at the bottom.
const readerScrollToActive = `
		const rows = [...document.querySelectorAll('.reader-row')];
		const i = rows.indexOf(document.querySelector('.reader-row.is-active'));
		document.querySelector('.reader-list').scrollTop = rows[i - 2].offsetTop - rows[0].offsetTop + 8;`

// shots is every screenshot the documentation uses. Each guide task adds its
// own; keep them grouped by guide and in page order.
//
// Several URLs name seeded rows by ID (/paste/3, /notes/25, /flash/2, ...).
// TestShotIDsPointAtTheIntendedItems in ../seed/seed_test.go pins what each
// ID must be; update it when adding or changing one here.
var shots = []shot{
	// docs/user/index.md
	{Name: "docs/user/images/login.png", URL: "/login", Anon: true, Width: 1280, Height: 640},
	{Name: "docs/user/images/dashboard.png", URL: "/"},
	{Name: "docs/user/images/user-menu.png", URL: "/", Setup: `document.querySelector('[data-shell-user-menu]').open = true;`},
	{Name: "docs/user/images/account.png", URL: "/account"},

	// docs/user/paste.md
	{Name: "docs/user/images/paste-list.png", URL: "/paste/3", Height: 600},
	{Name: "docs/user/images/paste-editor.png", URL: "/paste/new", Height: 640, Setup: `
		document.querySelector('input[name="title"]').value = 'Grocery list';
		document.querySelector('#new-language').value = 'markdown';
		document.querySelector('#new-body').value = '# This week\n\n- Milk\n- Bread\n- Apples\n- Pasta for Friday';`},
	{Name: "docs/user/images/paste-sharing.png", URL: "/paste/8", Width: 1600, Height: 420},
	{Name: "docs/user/images/paste-shared.png", URL: "{{share-paste}}", Anon: true, Height: 480},

	// docs/user/notes.md
	{Name: "docs/user/images/notes-outline.png", URL: "/notes/", Height: 720},
	{Name: "docs/user/images/notes-menu.png", URL: "/notes/", Height: 660, Setup: `
		const row = [...document.querySelectorAll('#outline .outline-row')].find(r => r.querySelector('input.outline-title').value.startsWith('Pick a paint'));
		row.querySelector('details.outline-menu').open = true;`},
	{Name: "docs/user/images/notes-zoomed.png", URL: "/notes/25", Height: 440},
	{Name: "docs/user/images/notes-due.png", URL: "/notes/due", Height: 640},
	{Name: "docs/user/images/notes-search.png", URL: "/notes/", Height: 480, Setup: `
		const box = document.getElementById('notes-search-input');
		box.value = 'kids';
		box.dispatchEvent(new Event('input', {bubbles: true}));
		await new Promise(r => setTimeout(r, 1200));
		box.blur();`},
	{Name: "docs/user/images/notes-share.png", URL: "{{share-notes}}", Anon: true, Height: 640},

	// docs/user/reader.md
	// Item 10 is already read in the seed, so opening it changes no counts.
	{Name: "docs/user/images/reader-three-pane.png", URL: "/reader/item/10?scope=all&filter=all", Height: 600, Setup: readerScrollToActive},
	{Name: "docs/user/images/reader-add-feed.png", URL: "/reader/", Height: 480, Setup: `
		document.getElementById('add-feed-dialog').showModal();
		document.getElementById('feed-url').value = 'https://example.org/';
		const folder = document.getElementById('feed-folder');
		folder.value = [...folder.options].find(o => o.text === 'Science').value;
		document.getElementById('feed-url').blur();`},
	{Name: "docs/user/images/reader-mark-read.png", URL: "/reader/", Height: 360, Setup: `
		document.querySelector('details.reader-mark-all-menu').open = true;`},
	{Name: "docs/user/images/reader-stats.png", URL: "/reader/stats", Height: 660},

	// docs/user/flash.md
	// Nothing here grades a card or answers sam's pending gift: the review
	// shot only turns the card over, so due counts and stats stay as seeded.
	{Name: "docs/user/images/flash-home.png", URL: "/flash/1", Height: 460},
	{Name: "docs/user/images/flash-cards.png", URL: "/flash/2/cards/", Height: 520},
	{Name: "docs/user/images/flash-editor.png", URL: "/flash/2/cards/new", Height: 675, Setup: `
		document.getElementById('card-type-new-cloze').checked = true;
		const front = document.getElementById('card-front-new');
		front.value = 'The {{c1::Monaco}} Grand Prix runs through the streets of Monte Carlo';
		front.blur();`},
	{Name: "docs/user/images/flash-review.png", URL: "/flash/review/2", Height: 490, Setup: `
		document.getElementById('review-flip').checked = true;
		await new Promise(r => setTimeout(r, 800));`},
	{Name: "docs/user/images/flash-share.png", URL: "/flash/1", Height: 300, Setup: `
		document.querySelector('details.flash-share-menu').open = true;`},
	{Name: "docs/user/images/flash-import.png", URL: "/flash/import", Height: 590},
	{Name: "docs/user/images/flash-stats.png", URL: "/flash/stats", Height: 760},

	// docs/user/admin.md
	// Look only: nothing here adds a user, resets a password or presses
	// Run now, so the seeded accounts and job history stay as they are.
	{Name: "docs/user/images/admin-overview.png", URL: "/admin/", Height: 500},
	{Name: "docs/user/images/admin-users.png", URL: "/admin/users", Height: 720, Setup: `
		document.querySelector('tr[data-user="sam"] details.usermgmt-menu').open = true;`},
	{Name: "docs/user/images/admin-jobs.png", URL: "/admin/jobs", Height: 770},

	// README.md — the hero and one thumbnail per app, light and dark, at the
	// default 1280×800 (thumbnails show at about half width).
	// The hero is the dashboard, cropped: it shows all four apps at a glance.
	{Name: "docs/images/hero-light.png", URL: "/", Height: 440},
	{Name: "docs/images/hero-dark.png", URL: "/", Height: 440, Theme: "dark"},
	{Name: "docs/images/app-paste-light.png", URL: "/paste/7"},
	{Name: "docs/images/app-paste-dark.png", URL: "/paste/7", Theme: "dark"},
	{Name: "docs/images/app-notes-light.png", URL: "/notes/"},
	{Name: "docs/images/app-notes-dark.png", URL: "/notes/", Theme: "dark"},
	{Name: "docs/images/app-reader-light.png", URL: "/reader/item/10?scope=all&filter=all", Setup: readerScrollToActive},
	{Name: "docs/images/app-reader-dark.png", URL: "/reader/item/10?scope=all&filter=all", Theme: "dark", Setup: readerScrollToActive},
	{Name: "docs/images/app-flash-light.png", URL: "/flash/2/cards/"},
	{Name: "docs/images/app-flash-dark.png", URL: "/flash/2/cards/", Theme: "dark"},
}
