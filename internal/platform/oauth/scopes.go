package oauth

// AllScopes is every scope the authorization server knows: a read and a
// write scope per app that AI access exposes. All eight are advertised as
// scopes_supported from the start, even before an app has tools, so nobody
// has to reconnect when later phases ship (spec
// 2026-11-24-ai-access-mcp-design.md, "Scopes"). A client is never given a
// scope outside this list.
var AllScopes = []string{
	"books:read", "books:write",
	"notes:read", "notes:write",
	"reader:read", "reader:write",
	"later:read", "later:write",
}

// ReadScopes are the four read-only scopes, which the consent screen ticks by
// default (spec: "Only allow what you need").
var ReadScopes = []string{"books:read", "notes:read", "reader:read", "later:read"}

// ScopeGroup is one row of the consent screen: an app with its read and
// write scope labels. The scopes are App's lowercase name plus ":read" and
// ":write".
type ScopeGroup struct {
	App, ReadLabel, WriteLabel string
}

// ScopeGroups is the consent screen's copy, worded as the spec's mock-up
// has it, in the order the rows appear.
var ScopeGroups = []ScopeGroup{
	{App: "Books", ReadLabel: "Read your books", WriteLabel: "Update books"},
	{App: "Notes", ReadLabel: "Read your notes", WriteLabel: "Add notes"},
	{App: "Reader", ReadLabel: "Read articles", WriteLabel: "Mark read / star"},
	{App: "Later", ReadLabel: "Read saved pages", WriteLabel: "Save / archive"},
}
