package api

import "testing"

func TestRate(t *testing.T) {
	if rate(0, 0) != nil {
		t.Fatal("nothing decided should give no rate")
	}
	if r := rate(1, 1); r == nil || *r != 0.5 {
		t.Fatalf("rate(1,1) = %v", r)
	}
	if r := rate(2, 0); r == nil || *r != 1 {
		t.Fatalf("rate(2,0) = %v", r)
	}
	if r := rate(0, 3); r == nil || *r != 0 {
		t.Fatalf("rate(0,3) = %v", r)
	}
}
