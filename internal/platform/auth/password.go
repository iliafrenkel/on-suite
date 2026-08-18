// Package auth owns user identity: password hashing, the users and sessions
// tables, and the HTTP glue that turns a cookie into a known user.
//
// password.go is the innermost layer and deliberately knows nothing about
// storage or HTTP, so it can be tested in isolation.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// MinPasswordLength is deliberately generous rather than clever: for a
// handful of trusted users, length beats composition rules.
const MinPasswordLength = 12

// ErrMalformedHash means a stored hash could not be parsed. Because stored
// hashes come from the database, this is treated as an error rather than as a
// failed login — it signals corruption, not a wrong password.
var ErrMalformedHash = errors.New("auth: malformed password hash")

// hashParams are the Argon2id cost parameters used for new hashes. Existing
// hashes carry their own parameters in their PHC string, so these can be
// raised later without invalidating anything already stored.
type hashParams struct {
	memory  uint32 // KiB
	time    uint32 // iterations
	threads uint8
	keyLen  uint32
	saltLen uint32
}

var defaultHashParams = hashParams{
	memory:  64 * 1024, // 64 MiB
	time:    3,
	threads: 4,
	keyLen:  32,
	saltLen: 16,
}

// ValidatePassword enforces policy on a new password. It is separate from
// HashPassword on purpose: HashPassword will faithfully hash an empty string,
// and the decision about what is acceptable belongs to the caller creating an
// account, not to the hashing primitive.
func ValidatePassword(plain string) error {
	if strings.TrimSpace(plain) == "" {
		return fmt.Errorf("password must not be blank")
	}
	if utf8.RuneCountInString(plain) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// HashPassword returns a PHC-format Argon2id hash, e.g.
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>
//
// The parameters travel with the hash, so VerifyPassword keeps working when
// defaultHashParams changes.
func HashPassword(plain string) (string, error) {
	p := defaultHashParams

	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, p.time, p.memory, p.threads, p.keyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether plain matches encoded.
//
// A false return with a nil error means "wrong password". A non-nil error
// means the stored hash could not be used at all.
func VerifyPassword(encoded, plain string) (bool, error) {
	// A PHC string starts with '$', so Split yields an empty first element.
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrMalformedHash
	}
	if version != argon2.Version {
		return false, fmt.Errorf("%w: unsupported version %d", ErrMalformedHash, version)
	}

	var p hashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrMalformedHash
	}
	if len(salt) == 0 || len(want) == 0 || p.memory == 0 || p.time == 0 || p.threads == 0 {
		return false, ErrMalformedHash
	}

	got := argon2.IDKey([]byte(plain), salt, p.time, p.memory, p.threads, uint32(len(want)))

	// Constant time, so a timing measurement cannot reveal how much of the
	// hash matched.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is a real Argon2id hash of a value nobody will guess. It exists so
// a login attempt for an unknown username can perform the same work as one for
// a real account, making the two indistinguishable by response time.
//
// Generated with the default parameters; the plaintext is irrelevant.
var dummyHash = mustHash("timing-equalisation-placeholder")

func mustHash(s string) string {
	h, err := HashPassword(s)
	if err != nil {
		panic("auth: hashing the dummy password failed: " + err.Error())
	}
	return h
}

// DummyVerify spends roughly the same time as VerifyPassword would, and
// discards the result. Call it when a username was not found.
func DummyVerify(password string) {
	_, _ = VerifyPassword(dummyHash, password)
}
