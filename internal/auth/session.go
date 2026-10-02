package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MinSecretLength is the shortest SESSION_SECRET accepted.
const MinSecretLength = 32

var (
	ErrSecretShort    = errors.New("the session secret must be at least 32 characters")
	ErrSessionInvalid = errors.New("the session token is not valid")
	ErrSessionExpired = errors.New("the session has expired")
)

// Session is what the cookie carries: who, which generation of their
// credentials, and when it was issued. AuthVersion is compared with the
// account's current value on every request, so a password change or a
// disabled account ends every session at once.
type Session struct {
	UserID      uuid.UUID
	AuthVersion int32
	IssuedAt    time.Time
}

type sessionPayload struct {
	U uuid.UUID `json:"u"`
	V int32     `json:"v"`
	T int64     `json:"t"`
}

// Signer encodes sessions as signed tokens (HMAC-SHA256).
type Signer struct {
	key []byte
}

// NewSigner derives the signing key from the secret.
func NewSigner(secret string) (*Signer, error) {
	if len(secret) < MinSecretLength {
		return nil, ErrSecretShort
	}
	sum := sha256.Sum256([]byte("writersguild-session:" + secret))
	return &Signer{key: sum[:]}, nil
}

const tokenVersion = "v1"

// Encode produces "v1.<payload>.<signature>", both parts base64url.
func (s *Signer) Encode(sess Session) string {
	raw, _ := json.Marshal(sessionPayload{U: sess.UserID, V: sess.AuthVersion, T: sess.IssuedAt.Unix()})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return tokenVersion + "." + payload + "." + s.sign(payload)
}

// Decode verifies the signature and the age of a token.
func (s *Signer) Decode(token string, now time.Time, maxAge time.Duration) (Session, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return Session{}, ErrSessionInvalid
	}
	if !hmac.Equal([]byte(parts[2]), []byte(s.sign(parts[1]))) {
		return Session{}, ErrSessionInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Session{}, ErrSessionInvalid
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.U == uuid.Nil {
		return Session{}, ErrSessionInvalid
	}
	issued := time.Unix(p.T, 0)
	if issued.After(now.Add(5 * time.Minute)) {
		return Session{}, ErrSessionInvalid
	}
	if maxAge > 0 && now.Sub(issued) > maxAge {
		return Session{}, ErrSessionExpired
	}
	return Session{UserID: p.U, AuthVersion: p.V, IssuedAt: issued}, nil
}

func (s *Signer) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
