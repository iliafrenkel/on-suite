// Toolbar/menu/dialog icon lookup, shared by any app's toolbar buttons —
// today only ON Reader, but the map lives here (rather than in
// internal/apps/reader) so a later pass can point Notes/Paste's own inline
// toolbar SVGs at it too without duplicating markup (see PATTERNS.md).
//
// Each entry is a complete, ready-to-use <svg class="toolbar-icon" ...> —
// unlike IconFor's bare tile icons, these already carry the class that
// app.css's shared `.toolbar-icon` rule (size, stroke, currentColor) keys
// off, so a template just does {{ticon "plus"}} with nothing else to add.

package ui

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
	// same meaning ("done"), kept visually identical across apps.
	"check": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 12l5 5L20 6"/>
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
	"doc": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M7 3h7l4 4v14H7z"/>
		<path d="M14 3v4h4"/>
	</svg>`,
	"inbox": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<path d="M4 12h4l2 3h4l2-3h4"/>
		<path d="M4 12l1.5-7h13L20 12v7H4z"/>
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
