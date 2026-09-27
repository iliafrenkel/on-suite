package usermgmt

import (
	"net/http"
	"strconv"

	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// accountPage is the view model for account.html. It never carries a
// password: a rejected form comes back with every password field empty.
type accountPage struct {
	Username string
	IsAdmin  bool
	Joined   string
	Error    string
	// Changed shows the confirmation after the post-change redirect.
	Changed bool
}

func (h *handlers) account(w http.ResponseWriter, r *http.Request) {
	h.renderAccount(w, r, http.StatusOK, accountPage{Changed: r.URL.Query().Get("changed") == "1"})
}

func (h *handlers) renderAccount(w http.ResponseWriter, r *http.Request, status int, data accountPage) {
	me, _ := web.UserFrom(r.Context())
	data.Username, data.IsAdmin, data.Joined = me.Username, me.IsAdmin, me.CreatedAt.Format("2006-01-02")
	page := h.page(r, "Account", "")
	page.Data = data
	if err := h.d.Render.Page(w, status, "account", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

// changePassword keeps the session that made the change and revokes every
// other one, so a stolen cookie stops working without signing the user out
// of the browser they are using (spec §5).
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	me, _ := web.UserFrom(r.Context())
	current, next, confirm := r.PostFormValue("current"), r.PostFormValue("new"), r.PostFormValue("confirm")
	reject := func(msg string) {
		h.renderAccount(w, r, http.StatusUnprocessableEntity, accountPage{Error: msg})
	}

	// Checked before the verify, so a locked-out caller costs no Argon2id
	// work. Keyed by user id, not IP: whoever holds a stolen cookie can
	// change address, but not whose account the cookie is for (#397).
	key := strconv.FormatInt(me.ID, 10)
	if !h.passwordAttempts.Allow(key, h.d.Now()) {
		h.d.Log.Warn("account password rate limit reached", "username", me.Username)
		h.d.Errors.Status(w, r, http.StatusTooManyRequests)
		return
	}
	ok, err := auth.VerifyPassword(me.PasswordHash, current)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	if !ok {
		h.passwordAttempts.Record(key, h.d.Now())
		reject("Your current password is incorrect.")
		return
	}
	h.passwordAttempts.Clear(key)
	if next != confirm {
		reject("The new passwords don't match.")
		return
	}
	if err := auth.ValidatePassword(next); err != nil {
		// ValidatePassword says "password must be ..."; prefixing "New "
		// makes it read as a sentence about this field.
		reject("New " + err.Error() + ".")
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	keep := ""
	if c, err := r.Cookie(web.SessionCookieName); err == nil {
		keep = c.Value
	}
	if err := h.d.Users.SetPassword(r.Context(), me.ID, hash, keep); err != nil {
		h.d.Errors.Internal(w, r, err)
		return
	}
	h.audit(r, "account.change_password", me.Username)
	http.Redirect(w, r, "/account?changed=1", http.StatusSeeOther)
}
