package reader

// AllowPrivateFetchesForTest lets this app's HTTP client reach loopback, so
// handler tests can point it at an httptest origin. Production never calls it;
// the real guard is what TestDefaultClientRefusesPrivateAddresses exercises.
//
// It must be called after Mount, which is where a.client is built —
// apptest.NewServer has already mounted by the time it hands the App back.
//
// This lives in a _test.go file (the standard Go export_test.go idiom) so it
// is compiled into the test binary — where package reader_test can still call
// it, since Go links internal and external test files together — but never
// into the production binary.
func (a *App) AllowPrivateFetchesForTest() {
	a.client.DenyAddr = func(string) error { return nil }
}
