package main

// shots is every screenshot the documentation uses. Each guide task adds its
// own; keep them grouped by guide and in page order.
var shots = []shot{
	// docs/user/index.md
	{Name: "docs/user/images/login.png", URL: "/login", Anon: true, Width: 1280, Height: 640},
	{Name: "docs/user/images/dashboard.png", URL: "/"},
	{Name: "docs/user/images/user-menu.png", URL: "/", Setup: `document.querySelector('[data-shell-user-menu]').open = true;`},
	{Name: "docs/user/images/account.png", URL: "/account"},
}
