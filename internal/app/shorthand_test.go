package app

import "testing"

func TestExpandShorthand(t *testing.T) {
	tests := []struct {
		in    string
		local bool
		want  string
	}{
		{"acme-org/multiverse", false, "https://github.com/acme-org/multiverse.git"},
		{"acme-org/multiverse.git", false, "https://github.com/acme-org/multiverse.git"},
		{"git@github.com:acme/x.git", false, "git@github.com:acme/x.git"},
		{"https://example.test/acme/x.git", false, "https://example.test/acme/x.git"},
		{"./acme/x", false, "./acme/x"},
		{"/abs/acme/x", false, "/abs/acme/x"},
		{"acme/x", true, "acme/x"}, // a directory that exists wins over the shorthand
		{"justaname", false, "justaname"},
		{"", false, ""},
	}
	for _, tt := range tests {
		if got := expandShorthand(tt.in, tt.local); got != tt.want {
			t.Errorf("expandShorthand(%q, %v) = %q, want %q", tt.in, tt.local, got, tt.want)
		}
	}
}
