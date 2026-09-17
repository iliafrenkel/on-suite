package flash

import "testing"

func TestURLHashIsStableAndDistinct(t *testing.T) {
	a := urlHash("https://example.com/cat.jpg")
	b := urlHash("https://example.com/cat.jpg")
	c := urlHash("https://example.com/dog.jpg")
	if a != b {
		t.Errorf("urlHash is not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("urlHash collided for different URLs")
	}
	if !validMediaHash(a) {
		t.Errorf("urlHash produced an invalid-shaped hash: %q", a)
	}
}

func TestContentHashIsStableAndDistinct(t *testing.T) {
	a := contentHash([]byte("hello"))
	b := contentHash([]byte("hello"))
	c := contentHash([]byte("world"))
	if a != b {
		t.Errorf("contentHash is not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("contentHash collided for different content")
	}
	if !validMediaHash(a) {
		t.Errorf("contentHash produced an invalid-shaped hash: %q", a)
	}
}

func TestValidMediaHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
		want bool
	}{
		{"real hash", urlHash("https://example.com/x"), true},
		{"too short", "abc", false},
		{"uppercase", "A" + urlHash("x")[1:], false},
		{"non-hex", "g" + urlHash("x")[1:], false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validMediaHash(tt.hash); got != tt.want {
				t.Errorf("validMediaHash(%q) = %v, want %v", tt.hash, got, tt.want)
			}
		})
	}
}

func TestContentTypeMatchesKind(t *testing.T) {
	tests := []struct {
		contentType string
		kind        string
		want        bool
	}{
		{"image/jpeg", MediaKindImage, true},
		{"image/png", MediaKindImage, true},
		{"audio/mpeg", MediaKindAudio, true},
		{"audio/wav", MediaKindAudio, true},
		{"audio/mpeg", MediaKindImage, false},
		{"image/jpeg", MediaKindAudio, false},
		{"text/html", MediaKindImage, false},
		{"image/jpeg", "video", false},
	}
	for _, tt := range tests {
		if got := contentTypeMatchesKind(tt.contentType, tt.kind); got != tt.want {
			t.Errorf("contentTypeMatchesKind(%q, %q) = %v, want %v", tt.contentType, tt.kind, got, tt.want)
		}
	}
}
