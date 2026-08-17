package backend

import (
	"strings"
	"testing"
)

func TestGeneratePasswordComplexity(t *testing.T) {
	pw, err := generatePassword(24)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 24 {
		t.Fatalf("len=%d, want 24", len(pw))
	}
	if !containsAny(pw, passwordUpper) || !containsAny(pw, passwordLower) ||
		!containsAny(pw, passwordDigits) || !containsAny(pw, passwordSpecial) {
		t.Fatalf("password %q missing required character class", pw)
	}
}

func TestGeneratePasswordUnique(t *testing.T) {
	a, err := generatePassword(16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := generatePassword(16)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two generated passwords should not be equal")
	}
}

func containsAny(s, class string) bool {
	return strings.ContainsAny(s, class)
}
