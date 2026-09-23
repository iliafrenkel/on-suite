package ui

// Toolbar/menu/dialog icon lookup, shared by any app's toolbar buttons —
// today only ON Reader, but the map lives here (rather than in
// internal/apps/reader) so a later pass can point Notes/Paste's own inline
// toolbar SVGs at it too without duplicating markup (see PATTERNS.md).
//
// Each entry is a complete, ready-to-use <svg class="toolbar-icon" ...> —
// unlike IconFor's bare tile icons, these already carry the class that
// app.css's shared `.toolbar-icon` rule (size, stroke, currentColor) keys
// off, so a template just does {{ticon "plus"}} with nothing else to add.

import "html/template"

var toolbarIcons = map[string]template.HTML{
	"more": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="5" cy="12" r="1.5" fill="currentColor" stroke="none"/>
		<circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/>
		<circle cx="19" cy="12" r="1.5" fill="currentColor" stroke="none"/>
	</svg>`,
	"plus": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 5v14M5 12h14"/>
	</svg>`,
	"refresh": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M20 11a8 8 0 1 0-2.3 5.7"/>
		<path d="M20 5v6h-6"/>
	</svg>`,
	"import": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4v11M8 11l4 4 4-4"/>
		<path d="M4 18h16"/>
	</svg>`,
	"export": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 15V4M8 8l4-4 4 4"/>
		<path d="M4 18h16"/>
	</svg>`,
	"folder": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 6h5l2 2h9v10H4z"/>
	</svg>`,
	"keyboard": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 7h18v10H3z"/>
		<path d="M6 11h.01M9 11h.01M12 11h.01M15 11h.01M18 11h.01M7 14h10"/>
	</svg>`,
	"stats": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M5 20V10M12 20V4M19 20v-7"/>
	</svg>`,
	"close": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M6 6l12 12M18 6L6 18"/>
	</svg>`,
	// Mirrors Notes' show-completed-toggle checkmark
	// (internal/apps/notes/templates/toolbar.partial.html) — same glyph,
	// same meaning ("done"), kept visually identical across apps. Used for
	// "Mark read"; "check-circle" below is its "Mark unread" counterpart
	// (issue #250), the same filled/outline-style pairing star-filled/
	// star-outline use, just as two line glyphs rather than a fill toggle —
	// a checkmark's fill has nothing to invert the way a star's does.
	"check": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 12l5 5L20 6"/>
	</svg>`,
	// "Mark unread": a checkmark circled, reading as "already confirmed
	// done — click to undo" rather than repeating the bare "Mark read" glyph
	// for the opposite action.
	"check-circle": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="12" cy="12" r="9"/>
		<path d="M8 12l3 3 5-6"/>
	</svg>`,
	"star-filled": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4l2.4 5.8L20.6 10l-4.6 4 1.4 6.2L12 17l-5.4 3.2L8 14l-4.6-4 6.2-.2z" fill="currentColor" stroke="none"/>
	</svg>`,
	"star-outline": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 4l2.4 5.8L20.6 10l-4.6 4 1.4 6.2L12 17l-5.4 3.2L8 14l-4.6-4 6.2-.2z"/>
	</svg>`,
	"external": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M9 6h9v9M18 6L7 17"/>
	</svg>`,
	// "Fetch full article" (the initial action, before either version of the
	// toggle below exists). "Show full article" uses "expand" and "Show feed
	// version" uses "rss" instead of reusing this — issue #250: all three
	// used to share "doc", making them indistinguishable in the same bar.
	"doc": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M7 3h7l4 4v14H7z"/>
		<path d="M14 3v4h4"/>
	</svg>`,
	// "Show full article": four corner brackets, the conventional
	// "expand/view full content" glyph.
	"expand": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M9 3H3v6M15 3h6v6M9 21H3v-6M15 21h6v-6"/>
	</svg>`,
	"inbox": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 12h4l2 3h4l2-3h4"/>
		<path d="M4 12l1.5-7h13L20 12v7H4z"/>
	</svg>`,
	"eye-off": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 3l18 18"/>
		<path d="M10.6 5.1A10.6 10.6 0 0 1 12 5c6 0 9.5 5.5 9.9 7a11.6 11.6 0 0 1-3.1 4.3M6.5 6.6C3.9 8.2 2.4 11 2.1 12c.3 1 1.5 3.1 3.5 4.8A10.4 10.4 0 0 0 12 19c1 0 1.9-.1 2.8-.4"/>
		<path d="M9.9 10a3 3 0 0 0 4.2 4.2"/>
	</svg>`,
	// Also doubles as the article pane's "Show feed version" action — going
	// back to the feed's own content reads naturally as the feed glyph.
	"rss": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none"/>
		<path d="M4 11a9 9 0 0 1 9 9"/>
		<path d="M4 4a16 16 0 0 1 16 16"/>
	</svg>`,
	// A panel with its left third split off, mirroring the sidebar it
	// toggles — the vertical divider is what reads as "sidebar" rather than
	// a generic square. .sidebar-toggle's own rotate(180deg) when collapsed
	// (app.css) flips the divider to the right side, so the same glyph shows
	// which way the next click will go instead of staying static.
	"sidebar-toggle": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<rect x="3" y="4" width="18" height="16" rx="2"/>
		<path d="M9 4v16"/>
	</svg>`,
	"edit": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M12 20h9"/>
		<path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/>
	</svg>`,
	"trash": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M3 6h18"/>
		<path d="M8 6V4h8v2"/>
		<path d="M19 6l-1 14H6L5 6"/>
	</svg>`,
	"cards": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<rect x="3" y="7" width="13" height="14" rx="2"/>
		<path d="M8 3h11a2 2 0 0 1 2 2v12"/>
	</svg>`,
	"play": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M7 4l13 8-13 8Z"/>
	</svg>`,
	"arrow-left": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M19 12H5"/>
		<path d="M12 19l-7-7 7-7"/>
	</svg>`,
	"share": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="18" cy="5" r="3"/>
		<circle cx="6" cy="12" r="3"/>
		<circle cx="18" cy="19" r="3"/>
		<path d="M8.6 13.5l6.8 4M15.4 6.5l-6.8 4"/>
	</svg>`,
}

// ToolbarIconFor returns the markup for a known toolbar/menu/dialog icon
// name, or an empty string for anything else — unlike IconFor's tile
// fallback, there is no sensible generic glyph for an unnamed action, so a
// typo'd name renders as nothing rather than a misleading placeholder.
func ToolbarIconFor(name string) template.HTML {
	if svg, ok := toolbarIcons[name]; ok {
		return svg
	}
	return ""
}
