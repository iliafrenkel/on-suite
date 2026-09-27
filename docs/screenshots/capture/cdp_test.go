package main

import (
	"encoding/json"
	"testing"
)

func TestResponseMatchesRequestIDAndSkipsEvents(t *testing.T) {
	msgs := []string{
		`{"method":"Page.loadEventFired","params":{}}`,
		`{"id":7,"result":{"data":"aGVsbG8="}}`,
	}
	got, err := awaitResult(7, func() ([]byte, error) {
		m := msgs[0]
		msgs = msgs[1:]
		return []byte(m), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var r struct{ Data string }
	if err := json.Unmarshal(got, &r); err != nil || r.Data != "aGVsbG8=" {
		t.Fatalf("result = %s, %v", got, err)
	}
}

func TestExpandURLSubstitutesSeedFiles(t *testing.T) {
	vars := map[string]string{"share-paste": "/paste/s/abc"}
	if got := expandURL("{{share-paste}}", vars); got != "/paste/s/abc" {
		t.Errorf("expandURL = %q", got)
	}
	if got := expandURL("/notes/", vars); got != "/notes/" {
		t.Errorf("plain URL changed: %q", got)
	}
}

func TestProtocolErrorIsReturned(t *testing.T) {
	_, err := awaitResult(3, func() ([]byte, error) {
		return []byte(`{"id":3,"error":{"code":-32000,"message":"boom"}}`), nil
	})
	if err == nil || err.Error() != "cdp: boom" {
		t.Fatalf("err = %v, want cdp: boom", err)
	}
}
