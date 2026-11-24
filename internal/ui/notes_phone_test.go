package ui_test

import "testing"

// TestNotesOutlineIsDenserOnPhones pins the phone layout of the Notes
// outline: a ~73px-per-level indent and three leading controls left level-4
// text 0px wide on a 375px screen (see
// docs/superpowers/specs/2026-11-24-notes-mobile-density-design.md).
func TestNotesOutlineIsDenserOnPhones(t *testing.T) {
	css := loadAppCSS(t)
	const phone = "@media (max-width: 780px)"
	// 20px per level; the guide line runs under the parent's chevron
	// (chevron is 1.25rem wide, so its centre is 0.625rem in).
	css.mustDeclare(phone, ".outline-item .outline-list", "margin-left: 0.625rem", "padding-left: 0.625rem")
	// The menu moves after the text; its drop-down opens from the right edge.
	css.mustDeclare(phone, ".outline-menu", "order: 1")
	css.mustDeclare(phone, ".outline-menu-list", "left: auto", "right: 0")
}
