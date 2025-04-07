package utilities

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		shouldErr bool
	}{
		// Valid package names
		{"valid simple name", "my-package", false},
		{"valid scoped name", "@user/package", false},
		{"valid name with numbers", "package123", false},
		{"valid name with underscores", "my_package", false},

		// Empty or malformed
		{"empty name", "", true},
		{"starts with underscore", "_badname", true},
		{"starts with dot", ".badname", true},
		{"contains whitespace", "bad name", true},
		{"contains tab", "bad\tname", true},
		{"contains newline", "bad\nname", true},
		{"contains capital", "baDname", true},
		{"too long", strings.Repeat("a", maxLen+1), true},

		// Blocklisted
		{"blocklist node_modules", "node_modules", true},
		{"blocklist favicon.ico", "favicon.ico", true},
		{"blocklist NODE_MODULES (case-insensitive)", "NODE_MODULES", true},

		// Disallowed characters
		{"contains tilde", "bad~name", true},
		{"contains single quote", "bad'name", true},
		{"contains exclamation", "bad!name", true},
		{"contains parentheses", "bad(name)", true},
		{"contains asterisk", "bad*name", true},
		{"contains double quote", `bad"name`, true},

		// Bad structure
		{"multiple slashes", "@user/package/extra", true},
		{"missing scope package", "@user/", true},
		{"just a slash", "/", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.input)
			if test.shouldErr && err == nil {
				t.Errorf("expected error for input '%s', but got nil", test.input)
			} else if !test.shouldErr && err != nil {
				t.Errorf("did not expect error for input '%s', but got: %v", test.input, err)
			}
		})
	}
}
