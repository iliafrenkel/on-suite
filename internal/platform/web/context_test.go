package web_test

import (
	"net/http/httptest"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

func TestIsHTMXHistoryRestore(t *testing.T) {
	req := httptest.NewRequest("GET", "/notes/due", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-History-Restore-Request", "true")
	if !web.IsHTMXHistoryRestore(req) {
		t.Error("a request carrying HX-History-Restore-Request must report true")
	}

	plain := httptest.NewRequest("GET", "/notes/due", nil)
	plain.Header.Set("HX-Request", "true")
	if web.IsHTMXHistoryRestore(plain) {
		t.Error("an ordinary HX-Request without the restore header must report false")
	}
}
