package docs

import (
	"embed"
	"io/fs"
)

//go:embed user
var user embed.FS

// User returns the user guides, rooted at docs/user: index.md, one page per
// app, admin.md and images/. internal/platform/help serves them at /help.
func User() fs.FS {
	sub, err := fs.Sub(user, "user")
	if err != nil {
		panic(err) // "user" is a literal embedded directory; this cannot fail
	}
	return sub
}
