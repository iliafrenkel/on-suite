package flash

// AllowPrivateFetchesForTest lets this app's HTTP client reach loopback, so
// handler tests can point it at an httptest origin. Production never calls
// it; the real guard is what TestDefaultMediaClientRefusesPrivateAddresses
// exercises.
//
// It must be called after Mount, which is where a.mediaClient is built —
// apptest.NewServer has already mounted by the time it hands the App back.
//
// This lives in a _test.go file (the standard Go export_test.go idiom) so it
// is compiled into the test binary — where package flash_test can still call
// it, since Go links internal and external test files together — but never
// into the production binary.
func (a *App) AllowPrivateFetchesForTest() {
	a.mediaClient.DenyAddr = func(string) error { return nil }
}

// MaxMediaFetchAttemptsForTest and MediaRetryBackoffForTest mirror
// handlers_media.go's unexported maxMediaFetchAttempts/mediaRetryBackoff, so
// a test can assert against the real thresholds rather than a hardcoded copy
// that could silently drift out of sync with them.
const (
	MaxMediaFetchAttemptsForTest = maxMediaFetchAttempts
	MediaRetryBackoffForTest     = mediaRetryBackoff
)
