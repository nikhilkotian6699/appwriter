// Package auth holds passwords, sessions and the login limiter.
package auth

import (
	"errors"
	"regexp"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLength = 8
	MinUsernameLength = 3
	MaxUsernameLength = 32
)

var (
	ErrUsernameFormat = errors.New("username must be 3-32 lower-case letters, digits, dots, hyphens or underscores")
	ErrPasswordShort  = errors.New("password must be at least 8 characters")
	ErrPasswordLong   = errors.New("password must be at most 72 bytes")

	usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// ValidateUsername enforces the brief: lower-case, 3 to 32 characters.
func ValidateUsername(u string) error {
	if len(u) < MinUsernameLength || len(u) > MaxUsernameLength || !usernameRe.MatchString(u) {
		return ErrUsernameFormat
	}
	return nil
}

// ValidatePassword enforces the minimum length. bcrypt only reads the first
// 72 bytes, so longer passwords are refused rather than silently truncated.
func ValidatePassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLength {
		return ErrPasswordShort
	}
	if len(p) > 72 {
		return ErrPasswordLong
	}
	return nil
}

// HashPassword returns a bcrypt hash at the default cost.
func HashPassword(p string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether p matches hash. It runs in constant time
// relative to the password contents.
func CheckPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

// DummyHash is compared against when a username does not exist, so that a
// wrong username costs the same time as a wrong password.
var DummyHash = func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("writers-guild-dummy-password"), bcrypt.DefaultCost)
	return string(h)
}()
