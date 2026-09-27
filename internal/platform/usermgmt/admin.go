package usermgmt

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// usersPage is the view model for admin_users.html.
type usersPage struct {
	Accounts []accountRow
	Error    string
	// Generated is set only on the response to an add or a reset: the one
	// and only time that password is shown.
	Generated *generatedPassword
	// Username and Admin refill the add form after a rejection.
	Username string
	Admin    bool
}

// accountRow projects auth.Account for the table, plus whether it is the
// viewer's own row, which gets no action menu (spec §4.4).
type accountRow struct {
	ID       int64
	Username string
	IsAdmin  bool
	Created  string
	Sessions int
	Self     bool
}

type generatedPassword struct {
	Username string
	Password string
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	h.renderUsers(w, r, http.StatusOK, usersPage{})
}

func (h *handlers) renderUsers(w http.ResponseWriter, r *http.Request, status int, data usersPage) {
	me, _ := web.UserFrom(r.Context())
	accounts, err := h.d.Users.ListAccounts(r.Context())
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	for _, a := range accounts {
		data.Accounts = append(data.Accounts, accountRow{
			ID:       a.ID,
			Username: a.Username,
			IsAdmin:  a.IsAdmin,
			Created:  a.CreatedAt.Format("2006-01-02"),
			Sessions: a.Sessions,
			Self:     a.ID == me.ID,
		})
	}
	if data.Generated != nil {
		// The password exists in this body and nowhere else; no cache may
		// keep a copy (spec §4.2).
		w.Header().Set("Cache-Control", "no-store")
	}
	page := h.page(r, "Users", "admin")
	page.Data = data
	if err := h.d.Render.Page(w, status, "admin_users", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) add(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	isAdmin := r.PostFormValue("admin") == "1"
	reject := func(msg string) {
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: msg, Username: username, Admin: isAdmin})
	}

	if err := auth.ValidateUsername(username); err != nil {
		reject("Usernames are 3–32 letters, digits, dots, dashes or underscores, starting and ending with a letter or digit.")
		return
	}
	password, hash, ok := h.newPassword(w, r)
	if !ok {
		return
	}
	u, err := h.d.Users.CreateUser(r.Context(), username, hash, isAdmin)
	if errors.Is(err, auth.ErrDuplicateUsername) {
		reject(fmt.Sprintf("The username %q is already taken.", username))
		return
	}
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	h.audit(r, "user.add", u.Username)
	h.renderUsers(w, r, http.StatusOK, usersPage{Generated: &generatedPassword{Username: u.Username, Password: password}})
}

// newPassword generates and hashes a one-time password. On failure it has
// already written the response.
func (h *handlers) newPassword(w http.ResponseWriter, r *http.Request) (password, hash string, ok bool) {
	password, err := auth.GeneratePassword()
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return "", "", false
	}
	hash, err = auth.HashPassword(password)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return "", "", false
	}
	return password, hash, true
}

// Filled in by Task 5.
func (h *handlers) resetPassword(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
func (h *handlers) setRole(w http.ResponseWriter, r *http.Request)       { h.d.Errors.NotFound(w, r) }
func (h *handlers) confirmDelete(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
func (h *handlers) delete(w http.ResponseWriter, r *http.Request)        { h.d.Errors.NotFound(w, r) }
