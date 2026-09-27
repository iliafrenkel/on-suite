package main

// shots is every screenshot the documentation uses. Each guide task adds its
// own; keep them grouped by guide and in page order.
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
}
