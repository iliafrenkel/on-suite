package oauth

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// activityRows is how many audit rows /account/assistants shows (spec
// 2026-11-24-ai-access-mcp-design.md, "Account and admin UI").
const activityRows = 50

// The spec asks for a section on /account and one on /admin. Those pages
// have no extension point, and usermgmt and admin must not import this
// package, so these are pages of their own, linked from a one-line entry on
// each (shown only when AI access is on). The content is the spec's.

// grantRow is one connection as a page shows it: a projection of Grant with
// only what the templates print (PATTERNS.md, view-model projection). The
// ID is the opaque grant ID the disconnect form posts back.
type grantRow struct {
	ID, Name, RedirectHost, Username string
	Access                           []accessLine
	Connected, LastUsed              string
}

// accessLine is what a grant may do in one app, in the consent screen's own
// words.
type accessLine struct {
	App    string
	Labels string
}

type activityRow struct {
	When, Assistant, Tool, Result string
	OK                            bool
}

type assistantsPage struct {
	Grants   []grantRow
	Activity []activityRow
}

type adminAIPage struct {
	PublicURL string
	Redirects []string
	Grants    []grantRow
}

// accessLines words the granted scopes per app, in the consent screen's
// order and labels. A scope outside ScopeGroups cannot be granted, so none
// is shown.
func accessLines(granted []string) []accessLine {
	var out []accessLine
	for _, g := range ScopeGroups {
		prefix := strings.ToLower(g.App)
		var labels []string
		if slices.Contains(granted, prefix+":read") {
			labels = append(labels, g.ReadLabel)
		}
		if slices.Contains(granted, prefix+":write") {
			labels = append(labels, g.WriteLabel)
		}
		if len(labels) > 0 {
			out = append(out, accessLine{App: g.App, Labels: strings.Join(labels, ", ")})
		}
	}
	return out
}

// grantRows and activityRowsOf show times in the server's zone, as the
// rest of the suite does (the store hands back UTC), in the admin page's
// layouts.
func grantRows(grants []Grant) []grantRow {
	rows := make([]grantRow, 0, len(grants))
	for _, g := range grants {
		row := grantRow{
			ID: g.ID, Name: g.ClientName, RedirectHost: g.RedirectHost, Username: g.Username,
			Access:    accessLines(g.Granted),
			Connected: g.CreatedAt.Local().Format("2006-01-02"),
			LastUsed:  "never",
		}
		if !g.LastUsedAt.IsZero() {
			row.LastUsed = g.LastUsedAt.Local().Format("2006-01-02 15:04 MST")
		}
		rows = append(rows, row)
	}
	return rows
}

func activityRowsOf(entries []AuditEntry) []activityRow {
	rows := make([]activityRow, 0, len(entries))
	for _, e := range entries {
		r := activityRow{
			When: e.At.Local().Format("2006-01-02 15:04:05 MST"), Assistant: e.ClientName, Tool: e.Tool,
			OK: e.OK, Result: "ok",
		}
		if !e.OK {
			r.Result = "error"
			if e.ErrorKind != "" {
				r.Result += ": " + e.ErrorKind
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// RedirectURIs is the redirect allowlist the server enforces, sorted, for
// the admin page.
func (s *Server) RedirectURIs() []string {
	out := make([]string, 0, len(s.redirects))
	for u := range s.redirects {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// mountPages adds the connected-assistants pages to Routes. The user pages
// sit behind RequireUser and always act on the signed-in user's own grants:
// someone else's grant ID is a 404, the same answer as one that does not
// exist. The admin pages sit behind RequireAdmin, which answers non-admins
// with 404 so the route's existence is not revealed. Every POST keeps the
// CSRF check.
func (s *Server) mountPages(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d PageDeps) {
	rec.Handle(mux, "GET /account/assistants", false, authn.RequireUser(s.assistants(d)))
	rec.Handle(mux, "POST /account/assistants/{id}/disconnect", false,
		authn.RequireUser(s.revokeGrant(d, false)))
	rec.Handle(mux, "GET /admin/ai", false, authn.RequireAdmin(s.adminAI(d)))
	rec.Handle(mux, "POST /admin/ai/{id}/revoke", false, authn.RequireAdmin(s.revokeGrant(d, true)))
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, d PageDeps, tmpl, title, activeApp string, data any) {
	r = r.WithContext(web.WithActiveApp(r.Context(), activeApp))
	page := app.NewPage(r, title, d.Nav)
	page.Shell.Version = d.Version
	page.Data = data
	if err := d.Render.Page(w, http.StatusOK, tmpl, page); err != nil {
		d.Errors.Internal(w, r, err)
	}
}

func (s *Server) assistants(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := web.UserFrom(r.Context())
		grants, err := s.store.Grants(r.Context(), me.ID)
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		audit, err := s.store.RecentAudit(r.Context(), me.ID, activityRows)
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		s.renderPage(w, r, d, "account_assistants", "Connected assistants", "",
			assistantsPage{Grants: grantRows(grants), Activity: activityRowsOf(audit)})
	}
}

func (s *Server) adminAI(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grants, err := s.store.Grants(r.Context(), 0)
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		s.renderPage(w, r, d, "admin_ai", "AI access", "admin", adminAIPage{
			PublicURL: s.publicURL, Redirects: s.RedirectURIs(), Grants: grantRows(grants),
		})
	}
}

// revokeGrant ends a grant and every token it issued, then returns to the
// page it was posted from. Users act only on their own grants; the admin
// (userID 0 to the store) on anyone's.
func (s *Server) revokeGrant(d PageDeps, admin bool) http.HandlerFunc {
	back := "/account/assistants"
	if admin {
		back = "/admin/ai"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := web.UserFrom(r.Context())
		owner := me.ID
		if admin {
			owner = 0
		}
		err := s.store.RevokeGrant(r.Context(), owner, r.PathValue("id"))
		if errors.Is(err, ErrNotFound) {
			d.Errors.NotFound(w, r)
			return
		}
		if err != nil {
			d.Errors.Internal(w, r, err)
			return
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
}
