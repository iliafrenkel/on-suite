// Package web is the HTTP layer of the platform: request context, middleware,
// CSRF, authentication and static assets. It may depend on auth and render;
// neither depends on it.
package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// Assets serves embedded static files under a prefix, with content-hashed
// URLs so they can be cached indefinitely and invalidated by deploying.
type Assets struct {
	fsys   fs.FS
	prefix string            // e.g. "/static"
	hashes map[string]string // "app.css" -> "1f3c9ab2"
}

// NewAssets hashes every file in fsys up front. The tree is embedded in the
// binary and cannot change at runtime, so hashing once at startup is both
// correct and cheap.
func NewAssets(fsys fs.FS, prefix string) (*Assets, error) {
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return nil, fmt.Errorf("web: asset prefix %q must start with /", prefix)
	}
	a := &Assets{
		fsys:   fsys,
		prefix: strings.TrimSuffix(prefix, "/"),
		hashes: make(map[string]string),
	}

	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sum, err := hashFile(fsys, name)
		if err != nil {
			return err
		}
		a.hashes[name] = sum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("web: hash assets: %w", err)
	}
	if len(a.hashes) == 0 {
		return nil, fmt.Errorf("web: no static assets found")
	}
	return a, nil
}

// URL returns the cache-busting URL for an asset, for use in templates.
// An unknown name yields a path with no version, which will 404 visibly
// rather than silently rendering a broken page.
func (a *Assets) URL(name string) string {
	name = strings.TrimPrefix(name, "/")
	if sum, ok := a.hashes[name]; ok {
		return a.prefix + "/" + name + "?v=" + sum
	}
	return a.prefix + "/" + name
}

// Names lists every asset, sorted. Used by tests.
func (a *Assets) Names() []string {
	out := make([]string, 0, len(a.hashes))
	for name := range a.hashes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Handler serves the assets. Mount it with http.StripPrefix(prefix, ...) so
// the paths it sees are relative to the tree root.
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))

		sum, ok := a.hashes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Only promise immutability when the caller asked for this exact
		// content. A bare URL must still revalidate, or a stale deploy would
		// be cached for a year.
		if r.URL.Query().Get("v") == sum {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("ETag", `"`+sum+`"`)

		// ServeFileFS handles content type, Range, If-None-Match and
		// If-Modified-Since against the ETag set above.
		http.ServeFileFS(w, r, a.fsys, name)
	})
}

func hashFile(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	// Eight hex characters is 32 bits: ample for cache busting, and short
	// enough to keep URLs readable in logs.
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}
