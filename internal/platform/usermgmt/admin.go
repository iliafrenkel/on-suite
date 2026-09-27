package usermgmt

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

// deletePage is the view model for admin_user_delete.html.
type deletePage struct {
	ID       int64
	Username string
}

// target resolves {id} to an account other than the viewer's. When it
// returns false it has already written the response: 404 for an id that is
// malformed or unknown, or the users page with selfMsg when the id is the
// viewer's own. The self check runs server-side, not only by hiding the menu
// (spec §4.4).
func (h *handlers) target(w http.ResponseWriter, r *http.Request, selfMsg string) (auth.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.d.Errors.NotFound(w, r)
		return auth.User{}, false
	}
	u, err := h.d.Users.UserByID(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		h.d.Errors.NotFound(w, r)
		return auth.User{}, false
	}
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return auth.User{}, false
	}
	if me, _ := web.UserFrom(r.Context()); u.ID == me.ID {
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: selfMsg})
		return auth.User{}, false
	}
	return u, true
}

// storeFailed maps a store error from a mutation to a response.
func (h *handlers) storeFailed(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		h.d.Errors.NotFound(w, r)
	case errors.Is(err, auth.ErrLastAdmin):
		h.renderUsers(w, r, http.StatusUnprocessableEntity, usersPage{Error: "At least one administrator must remain."})
	default:
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) resetPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "Use Account to change your own password.")
	if !ok {
		return
	}
	password, hash, ok := h.newPassword(w, r)
	if !ok {
		return
	}
	if err := h.d.Users.SetPassword(r.Context(), u.ID, hash, ""); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	h.audit(r, "user.reset_password", u.Username)
	h.renderUsers(w, r, http.StatusOK, usersPage{Generated: &generatedPassword{Username: u.Username, Password: password}})
}

func (h *handlers) setRole(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't change your own role.")
	if !ok {
		return
	}
	makeAdmin := r.PostFormValue("admin") == "1"
	if err := h.d.Users.SetAdmin(r.Context(), u.ID, makeAdmin); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	action := "user.demote"
	if makeAdmin {
		action = "user.promote"
	}
	h.audit(r, action, u.Username)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *handlers) confirmDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't delete yourself.")
	if !ok {
		return
	}
	page := h.page(r, "Delete "+u.Username, "admin")
	page.Data = deletePage{ID: u.ID, Username: u.Username}
	if err := h.d.Render.Page(w, http.StatusOK, "admin_user_delete", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) delete(w http.ResponseWriter, r *http.Request) {
	u, ok := h.target(w, r, "You can't delete yourself.")
	if !ok {
		return
	}
	if err := h.d.Users.DeleteUser(r.Context(), u.ID); err != nil {
		h.storeFailed(w, r, err)
		return
	}
	h.audit(r, "user.delete", u.Username)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
