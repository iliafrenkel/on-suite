package ui

// Icon lookup for the sidebar and home-page cards. Icons are looked up by
// app id rather than carried on app.Meta, so a "coming soon" placeholder for
// an app that does not exist yet in code can still get one.

import "html/template"

var icons = map[string]template.HTML{
	"paste": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M7 9h10M7 13h10M7 17h6" stroke="var(--c-accent)" stroke-width="1.8" stroke-linecap="round" fill="none"/>
	</svg>`,
	"notes": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="8" cy="9" r="1.4" fill="var(--c-accent)"/>
		<circle cx="8" cy="13" r="1.4" fill="var(--c-accent)"/>
		<circle cx="8" cy="17" r="1.4" fill="var(--c-accent)"/>
		<path d="M11.5 9h7M11.5 13h7M11.5 17h4" stroke="var(--c-accent)" stroke-width="1.8" stroke-linecap="round"/>
	</svg>`,
	"reader": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<circle cx="7" cy="17" r="1.6" fill="var(--c-accent)"/>
		<path d="M7 12.5a4.5 4.5 0 0 1 4.5 4.5" stroke="var(--c-accent)" stroke-width="1.8" fill="none" stroke-linecap="round"/>
		<path d="M7 8a9 9 0 0 1 9 9" stroke="var(--c-accent)" stroke-width="1.8" fill="none" stroke-linecap="round"/>
	</svg>`,
	"admin": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M12 6l5 2.2v3.4c0 3-2.1 5.2-5 6.4-2.9-1.2-5-3.4-5-6.4V8.2L12 6z" fill="none" stroke="var(--c-accent)" stroke-width="1.8" stroke-linejoin="round"/>
	</svg>`,
	"flash": `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
		<rect x="2" y="2" width="20" height="20" rx="5" fill="var(--c-accent-bg)"/>
		<path d="M13 6 8 13h4l-1 5 6-8h-4l1-4z" fill="var(--c-accent)"/>
	</svg>`,
}

const fallbackIcon = `<svg viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
	<rect x="1" y="1" width="10" height="10" rx="3" fill="var(--c-accent)"/>
	<rect x="13" y="1" width="10" height="10" rx="3" fill="var(--c-accent-bg)"/>
	<rect x="1" y="13" width="10" height="10" rx="3" fill="var(--c-accent-bg)"/>
	<rect x="13" y="13" width="10" height="10" rx="3" fill="var(--c-accent)"/>
</svg>`

// IconFor returns the icon markup for a known app id, or a generic tile icon
// for anything else. id is always a compile-time-known literal (an app's own
// Meta.ID or an entry in cmd/onsuite's coming-soon list), never user input, so
// returning it unescaped as template.HTML is safe.
func IconFor(id string) template.HTML {
	if svg, ok := icons[id]; ok {
		return svg
	}
	return template.HTML(fallbackIcon)
}
