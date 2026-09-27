package usermgmt

import "net/http"

// Filled in by Task 6.
func (h *handlers) account(w http.ResponseWriter, r *http.Request)        { h.d.Errors.NotFound(w, r) }
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) { h.d.Errors.NotFound(w, r) }
