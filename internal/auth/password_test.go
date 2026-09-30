package auth

import "testing"

func TestValidateUsername(t *testing.T) {
	ok := []string{"abc", "nikhil", "a.b-c_d", "user123", "abcdefghijklmnopqrstuvwxyz012345"}
	for _, u := range ok {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("%q should be valid: %v", u, err)
		}
	}
	bad := []string{"", "ab", "Nikhil", "with space", ".dot", "abcdefghijklmnopqrstuvwxyz0123456", "émile", "a/b"}
	for _, u := range bad {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("%q should be invalid", u)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err != ErrPasswordShort {
		t.Fatalf("want ErrPasswordShort, got %v", err)
	}
	if err := ValidatePassword("exactly8"); err != nil {
		t.Fatalf("8 chars should pass: %v", err)
	}
	if err := ValidatePassword("ünïcödé!"); err != nil {
		t.Fatalf("8 runes should pass: %v", err)
	}
	long := make([]byte, 73)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidatePassword(string(long)); err != ErrPasswordLong {
		t.Fatalf("want ErrPasswordLong, got %v", err)
	}
}

func TestHashAndCheck(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse battery") {
		t.Fatal("expected match")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("expected mismatch")
	}
	if !CheckPassword(DummyHash, "writers-guild-dummy-password") || CheckPassword(DummyHash, "x") {
		t.Fatal("dummy hash misbehaves")
	}
}
