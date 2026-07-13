package boxes

import (
	"testing"
)

func TestNoWrapHonorsNewlines(t *testing.T) {
	m := noWrap("FILTER BY COUNTRY\nPress a key\n\n  (1)  United States\n  (2)  Germany")
	if len(m) != 5 {
		t.Fatalf("expected 5 rows, got %d: %v", len(m), m)
	}
	if string(m[0]) != "FILTER BY COUNTRY" {
		t.Fatalf("row0: %q", string(m[0]))
	}
	if string(m[3]) != "  (1)  United States" {
		t.Fatalf("row3: %q", string(m[3]))
	}
	if string(m[4]) != "  (2)  Germany" {
		t.Fatalf("row4: %q", string(m[4]))
	}
}

func TestNoWrapSingleLine(t *testing.T) {
	m := noWrap("hello")
	if len(m) != 1 || string(m[0]) != "hello" {
		t.Fatalf("got %v", m)
	}
}

func TestNoWrapCRLF(t *testing.T) {
	m := noWrap("a\r\nb\rc")
	if len(m) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(m))
	}
	if string(m[0]) != "a" || string(m[1]) != "b" || string(m[2]) != "c" {
		t.Fatalf("got %q %q %q", string(m[0]), string(m[1]), string(m[2]))
	}
}
