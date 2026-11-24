package oauth

import (
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"authelia.com/provider/oauth2"
	"authelia.com/provider/oauth2/handler/cimd"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// Fields the consent form adds to the original authorization request. They
// are taken off the form before the library re-parses it on POST, so the
// decision and the CSRF token never end up stored inside a code or token
// session.
const (
	fieldDecision = "decision"
	fieldScope    = "grant_scope"
)

var consentOnlyFields = []string{fieldDecision, fieldScope, web.CSRFFormField}

// consentPage is the view model for oauth_consent.html. It carries only what
// the page shows, never the client or request objects (PATTERNS.md,
// view-model projection).
type consentPage struct {
	ClientName   string
	RedirectHost string
	Rows         []consentRow
	// Hidden is the original request, posted back as it arrived so the
	// decision is made on exactly the request the user saw.
	Hidden []hiddenField
}

// consentRow is one app's line. A nil box is a scope the client did not ask
// for, which is not shown at all (spec, "Consent screen").
type consentRow struct {
	App         string
	Read, Write *consentBox
}

type consentBox struct {
	Scope, Label string
	Checked      bool
}

type hiddenField struct{ Name, Value string }

// consentRows lays the requested scopes out per app. Reads come ticked and
// writes unticked, so the user opts into writes (spec, "Default ticks").
func consentRows(requested []string) []consentRow {
	var rows []consentRow
	for _, g := range ScopeGroups {
		prefix := strings.ToLower(g.App)
		row := consentRow{App: g.App}
		if sc := prefix + ":read"; slices.Contains(requested, sc) {
			row.Read = &consentBox{Scope: sc, Label: g.ReadLabel, Checked: true}
		}
		if sc := prefix + ":write"; slices.Contains(requested, sc) {
			row.Write = &consentBox{Scope: sc, Label: g.WriteLabel}
		}
		if row.Read != nil || row.Write != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

// hiddenFields is the request form in a stable order, less anything the
// consent form adds itself.
func hiddenFields(form url.Values) []hiddenField {
	keys := make([]string, 0, len(form))
	for k := range form {
		if !slices.Contains(consentOnlyFields, k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []hiddenField
	for _, k := range keys {
		for _, v := range form[k] {
			out = append(out, hiddenField{Name: k, Value: v})
		}
	}
	return out
}

// clientName is the name the client registered or published. It is only the
// client's claim, which is why the consent page puts the validated redirect
// host above it.
func clientName(c oauth2.Client) string {
	var rc *oauth2.DefaultRegisteredClient
	switch v := c.(type) {
	case *oauth2.DefaultRegisteredClient:
		rc = v
	case *cimd.Client:
		rc = v.DefaultRegisteredClient
	}
	if rc != nil {
		if name := strings.TrimSpace(rc.ClientName); name != "" {
			return name
		}
	}
	return "Unnamed assistant"
}

// defaultScope treats a request with no scope as asking for every read
// scope (spec, "Default ticks"). It must run before the library parses the
// request, which then validates those scopes like any others.
func defaultScope(r *http.Request) {
	if err := r.ParseForm(); err != nil {
		return // the library reports the same parse error as invalid_request
	}
	if strings.TrimSpace(r.Form.Get("scope")) == "" {
		r.Form.Set("scope", strings.Join(ReadScopes, " "))
	}
}

// parseAuthorize parses and validates an authorization request, answering
// the client itself when it is not one we will serve. Every token is for
// our MCP endpoint, so a request naming any other resource is refused
// rather than silently narrowed.
func (s *Server) parseAuthorize(w http.ResponseWriter, r *http.Request) (oauth2.AuthorizeRequester, bool) {
	ctx := r.Context()
	defaultScope(r)
	ar, err := s.provider.NewAuthorizeRequest(ctx, r)
	if err == nil {
		if res := ar.GetRequestedResource(); len(res) > 0 && !slices.Equal([]string(res), []string{s.ResourceURL()}) {
			err = oauth2.ErrInvalidTarget.WithHintf("The only resource this server issues tokens for is '%s'.", s.ResourceURL())
		}
	}
	if err != nil {
		s.provider.WriteAuthorizeError(ctx, w, ar, err)
		return nil, false
	}
	return ar, true
}

// authorize is GET /oauth/authorize. It always shows the consent page and
// never writes anything: a GET is reachable from any website by a plain
// link, and the session cookie is SameSite=Lax, so a GET that skipped
// consent would let any page silently re-authorize the user, replacing
// (and so disconnecting) their existing connection. Only the CSRF-checked
// POST records a grant (Ilia's decision in the Task 5 review; this
// overrides the spec's "Consent screen" skip rule).
func (s *Server) authorize(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ar, ok := s.parseAuthorize(w, r)
		if !ok {
			return
		}
		redirect := ar.GetRedirectURI()
		name := clientName(ar.GetClient())

		// The browser follows the consent POST's redirect to the client, and
		// form-action covers that redirect, so this page alone allows the
		// validated redirect's origin (spec, "Wiring": CSP).
		w.Header().Set("Content-Security-Policy", web.CSPWithFormAction(redirect.Scheme+"://"+redirect.Host))
		page := app.NewPage(r, "Allow access", d.Nav)
		page.Shell.Version = d.Version
		page.Data = consentPage{
			ClientName:   name,
			RedirectHost: redirect.Host,
			Rows:         consentRows(ar.GetRequestedScopes()),
			Hidden:       hiddenFields(ar.GetRequestForm()),
		}
		if err := d.Render.Page(w, http.StatusOK, "oauth_consent", page); err != nil {
			d.Errors.Internal(w, r, err)
		}
	}
}

// decide is POST /oauth/authorize, the consent decision. The CSRF check has
// already run (the route is not exempt): this is a logged-in browser form.
func (s *Server) decide(d PageDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := web.UserFrom(r.Context())
		decision := r.PostFormValue(fieldDecision)
		picked := r.PostForm[fieldScope]
		for _, k := range consentOnlyFields {
			r.Form.Del(k)
			r.PostForm.Del(k)
		}

		ar, ok := s.parseAuthorize(w, r)
		if !ok {
			return
		}
		if decision != "allow" {
			s.provider.WriteAuthorizeError(r.Context(), w, ar, oauth2.ErrAccessDenied.WithHint("The user cancelled the request."))
			return
		}

		// The page only offers requested scopes, so anything else is a forged
		// form, refused outright rather than dropped (review focus 5).
		requested := ar.GetRequestedScopes()
		var scopes []string
		for _, sc := range picked {
			if !slices.Contains(requested, sc) {
				d.Errors.Status(w, r, http.StatusBadRequest)
				return
			}
			if !slices.Contains(scopes, sc) {
				scopes = append(scopes, sc)
			}
		}
		if len(scopes) == 0 {
			s.provider.WriteAuthorizeError(r.Context(), w, ar, oauth2.ErrAccessDenied.WithHint("The user allowed no scopes."))
			return
		}

		s.issue(w, r, d, ar, Grant{
			ID:           ar.GetID(),
			UserID:       me.ID,
			ClientID:     ar.GetClient().GetID(),
			ClientName:   clientName(ar.GetClient()),
			RedirectHost: ar.GetRedirectURI().Host,
			Requested:    []string(requested),
			Granted:      scopes,
		}, scopes)
	}
}

// issue records the grant, then sends the browser back to the client with a
// code for scopes and our resource. The grant goes first because every code
// and token row references it by the request ID (migration 0003); if the
// code cannot be issued, the grant just written is taken back.
func (s *Server) issue(w http.ResponseWriter, r *http.Request, d PageDeps, ar oauth2.AuthorizeRequester, g Grant, scopes []string) {
	ctx := r.Context()
	if err := s.store.UpsertGrant(ctx, g); err != nil {
		d.Errors.Internal(w, r, err)
		return
	}
	for _, sc := range scopes {
		ar.GrantScope(sc)
	}
	ar.GrantResource(s.ResourceURL())
	resp, err := s.provider.NewAuthorizeResponse(ctx, ar, &oauth2.DefaultSession{Subject: strconv.FormatInt(g.UserID, 10)})
	if err != nil {
		if rerr := s.store.RevokeGrant(ctx, g.UserID, g.ID); rerr != nil && d.Log != nil {
			d.Log.Error("oauth: removing a grant whose code failed", "error", rerr)
		}
		s.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	s.provider.WriteAuthorizeResponse(ctx, w, ar, resp)
}
