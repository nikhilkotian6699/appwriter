package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "a-secret-that-is-long-enough-for-tests-0123456789"

func TestSessionRoundTrip(t *testing.T) {
	s, err := NewSigner(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	sess := Session{UserID: uuid.New(), AuthVersion: 3, IssuedAt: now}
	tok := s.Encode(sess)
	if !strings.HasPrefix(tok, "v1.") || strings.Count(tok, ".") != 2 {
		t.Fatalf("token shape %q", tok)
	}
	got, err := s.Decode(tok, now.Add(time.Hour), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != sess.UserID || got.AuthVersion != 3 || !got.IssuedAt.Equal(now) {
		t.Fatalf("decoded %+v", got)
	}
}

func TestSessionRejects(t *testing.T) {
	s, _ := NewSigner(testSecret)
	now := time.Now()
	tok := s.Encode(Session{UserID: uuid.New(), AuthVersion: 1, IssuedAt: now})
	parts := strings.Split(tok, ".")

	tampered := parts[0] + "." + parts[1][:len(parts[1])-2] + "AA." + parts[2]
	if _, err := s.Decode(tampered, now, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("tampered payload: %v", err)
	}
	badSig := parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))
	if _, err := s.Decode(badSig, now, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("bad signature: %v", err)
	}
	for _, bad := range []string{"", "v1", "v0." + parts[1] + "." + parts[2], "garbage.garbage.garbage"} {
		if _, err := s.Decode(bad, now, time.Hour); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	other, _ := NewSigner("another-secret-that-is-also-long-enough-9876543210")
	if _, err := other.Decode(tok, now, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("other key: %v", err)
	}
	if _, err := s.Decode(tok, now.Add(2*time.Hour), time.Hour); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := s.Decode(tok, now.Add(2*time.Hour), 0); err != nil {
		t.Fatalf("no max age should not expire: %v", err)
	}
	future := s.Encode(Session{UserID: uuid.New(), AuthVersion: 1, IssuedAt: now.Add(time.Hour)})
	if _, err := s.Decode(future, now, time.Hour); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("issued in the future: %v", err)
	}
	if _, err := NewSigner("short"); !errors.Is(err, ErrSecretShort) {
		t.Fatalf("short secret: %v", err)
	}
}

func TestLimiterSchedule(t *testing.T) {
	l := NewLimiter()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	l.Now = func() time.Time { return now }
	key := Key("Admin", "10.0.0.7")
	if key != "admin@10.0.0.7" {
		t.Fatalf("key %q", key)
	}
	for i := 1; i <= 4; i++ {
		if w := l.Fail(key); w != 0 {
			t.Fatalf("attempt %d should still be free, got wait %s", i, w)
		}
		if l.Check(key) != 0 {
			t.Fatalf("no wait expected after %d failures", i)
		}
	}
	if w := l.Fail(key); w != time.Minute {
		t.Fatalf("5th failure should lock for a minute, got %s", w)
	}
	if w := l.Check(key); w != time.Minute {
		t.Fatalf("check during lock %s", w)
	}
	now = now.Add(30 * time.Second)
	if w := l.Check(key); w != 30*time.Second {
		t.Fatalf("remaining wait %s", w)
	}
	now = now.Add(31 * time.Second)
	if l.Check(key) != 0 {
		t.Fatal("lock should have passed")
	}
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, w := range want {
		if got := l.Fail(key); got != w {
			t.Fatalf("failure %d: wait %s, want %s", 6+i, got, w)
		}
		now = now.Add(w)
	}
	// Another address is a different bucket; a right password resets.
	if l.Check(Key("admin", "10.0.0.8")) != 0 {
		t.Fatal("another address should not be locked")
	}
	l.Reset(key)
	if l.Check(key) != 0 || l.Fail(key) != 0 {
		t.Fatal("reset should start over")
	}
}

func TestLimiterSweep(t *testing.T) {
	l := NewLimiter()
	now := time.Now()
	l.Now = func() time.Time { return now }
	for i := 0; i < 300; i++ {
		l.Fail(Key("user", string(rune('a'+i%26))+"-"+time.Duration(i).String()))
	}
	now = now.Add(2 * time.Hour)
	for i := 0; i < 300; i++ {
		l.Check("x")
	}
	l.mu.Lock()
	n := len(l.entries)
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("idle buckets should be swept, %d remain", n)
	}
}
