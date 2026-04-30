// If you are AI: This file unit-tests the RTMP publish authenticator and stream-name parser.

package rtmp

import "testing"

// TestAuthenticatorAnonymous: nil authenticator allows everything.
func TestAuthenticatorAnonymous(t *testing.T) {
	var a *Authenticator
	if !a.Allow("") {
		t.Fatal("nil Authenticator should treat all publishes as anonymous-allowed")
	}
	if !a.Allow("anything") {
		t.Fatal("nil Authenticator should allow non-empty key as well")
	}
}

// TestAuthenticatorEmptyKeys: explicit empty list yields nil (anonymous).
func TestAuthenticatorEmptyKeys(t *testing.T) {
	if NewAuthenticator(nil) != nil {
		t.Fatal("nil keys should produce nil authenticator")
	}
	if NewAuthenticator([]string{"", "  "}) != nil {
		t.Fatal("whitespace-only keys should produce nil authenticator")
	}
}

// TestAuthenticatorAllow: matching keys succeed, non-matching fail.
func TestAuthenticatorAllow(t *testing.T) {
	a := NewAuthenticator([]string{"alpha", "bravo"})
	if a == nil {
		t.Fatal("expected authenticator")
	}
	cases := []struct {
		in   string
		want bool
	}{
		{"alpha", true},
		{"bravo", true},
		{"charlie", false},
		{"", false},
		{"alphaa", false}, // length-mismatch must not pass
	}
	for _, tc := range cases {
		if got := a.Allow(tc.in); got != tc.want {
			t.Errorf("Allow(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestParseStreamName covers the key= extraction and edge cases.
func TestParseStreamName(t *testing.T) {
	cases := []struct {
		raw, name, key string
	}{
		{"foo", "foo", ""},
		{"foo?key=secret", "foo", "secret"},
		{"foo?key=secret&extra=1", "foo", "secret"},
		{"foo?nothing=here", "foo", ""},
		{"foo?", "foo", ""},
		{"?key=only", "", "only"},
	}
	for _, tc := range cases {
		gotName, gotKey := ParseStreamName(tc.raw)
		if gotName != tc.name || gotKey != tc.key {
			t.Errorf("ParseStreamName(%q) = (%q,%q), want (%q,%q)",
				tc.raw, gotName, gotKey, tc.name, tc.key)
		}
	}
}
