package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"

	encoded, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("encoded = %q, want an argon2id PHC string", encoded)
	}
	if strings.Contains(encoded, pw) {
		t.Fatal("encoded hash contains the plaintext password")
	}

	ok, err := VerifyPassword(encoded, pw)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("correct password did not verify")
	}

	ok, err = VerifyPassword(encoded, "wrong password entirely")
	if err != nil {
		t.Fatalf("VerifyPassword on wrong password returned an error: %v", err)
	}
	if ok {
		t.Error("wrong password verified")
	}
}

// TestHashPasswordUsesARandomSalt guards against the classic mistake of a
// fixed salt, which would make the hashes rainbow-table-able.
func TestHashPasswordUsesARandomSalt(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password are identical, salt is not random")
	}
}

func TestHashPasswordHandlesAwkwardInput(t *testing.T) {
	tests := []struct{ name, pw string }{
		{"empty", ""},
		{"unicode", "пароль-סיסמה-🔐"},
		{"very long", strings.Repeat("x", 4096)},
		{"contains dollar signs", "a$b$c$argon2id$"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := HashPassword(tt.pw)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			ok, err := VerifyPassword(encoded, tt.pw)
			if err != nil {
				t.Fatalf("VerifyPassword: %v", err)
			}
			if !ok {
				t.Error("password did not verify after round trip")
			}
		})
	}
}

// TestVerifyPasswordRejectsMalformedHashes matters because these values come
// out of the database. A corrupted or hand-edited row must produce an error,
// never a successful login.
func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	tests := []struct{ name, encoded string }{
		{"empty", ""},
		{"not a phc string", "notahash"},
		{"wrong variant", "$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{"unsupported version", "$argon2id$v=99$m=65536,t=3,p=4$c2FsdA$aGFzaA"},
		{"bad base64 salt", "$argon2id$v=19$m=65536,t=3,p=4$!!!$aGFzaA"},
		{"zero parameters", "$argon2id$v=19$m=0,t=0,p=0$c2FsdA$aGFzaA"},
		{"missing fields", "$argon2id$v=19$m=65536,t=3,p=4"},
		{"bcrypt hash", "$2y$10$abcdefghijklmnopqrstuv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword(tt.encoded, "any password")
			if ok {
				t.Fatal("malformed hash verified successfully")
			}
			if !errors.Is(err, ErrMalformedHash) {
				t.Errorf("err = %v, want ErrMalformedHash", err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"long enough", strings.Repeat("a", MinPasswordLength), false},
		{"one short", strings.Repeat("a", MinPasswordLength-1), true},
		{"empty", "", true},
		{"whitespace only", strings.Repeat(" ", MinPasswordLength+5), true},
		{"unicode counted in runes not bytes", strings.Repeat("é", MinPasswordLength), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pw)
			if tt.wantErr && err == nil {
				t.Error("ValidatePassword accepted an invalid password")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePassword rejected a valid password: %v", err)
			}
		})
	}
}
