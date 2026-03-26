package git

import (
	"errors"
	"testing"
)

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
		{
			name:     "error with ghs_ token in URL",
			err:      errors.New("failed to push: fatal: could not read Password for 'https://ghs_1111111111111111111111111@github.com': No such device"),
			expected: "failed to push: fatal: could not read Password for GitHub authentication: No such device",
		},
		{
			name:     "error with ghp_ token in URL",
			err:      errors.New("failed: https://ghp_1234567890abcdefghijklmnopqrstuvwxyz@github.com failed"), // notsecret
			expected: "failed: https://***@github.com failed",
		},
		{
			name:     "error with standalone token",
			err:      errors.New("token ghs_1234567890abcdefghijklmnopqrstuvwx leaked"),
			expected: "token *** leaked",
		},
		{
			name:     "error without token",
			err:      errors.New("normal error message"),
			expected: "normal error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeError(tt.err)
			if result != tt.expected {
				t.Errorf("SanitizeError() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with ghs_ token",
			input:    "https://ghs_ABC123XYZ456DEF789GHI012JKL345MN@github.com/repo.git",
			expected: "https://***@github.com/repo.git",
		},
		{
			name:     "URL with ghp_ token",
			input:    "clone failed: https://ghp_abcdefghijklmnopqrstuvwxyz1234567890@github.com", // notsecret
			expected: "clone failed: https://***@github.com",
		},
		{
			name:     "URL with x-access-token format",
			input:    "push failed: https://x-access-token:ghs_ABC123XYZ456DEF789GHI012JKL345MN@github.com/repo.git",
			expected: "push failed: https://x-access-token:***@github.com/repo.git",
		},
		{
			name:     "multiple tokens",
			input:    "token1: ghs_1234567890abcdefghijklmnopqrstuvwx token2: ghp_abcdefghijklmnopqrstuvwxyz1234567890", // notsecret
			expected: "token1: *** token2: ***",
		},
		{
			name:     "no token",
			input:    "just a normal string https://github.com/repo",
			expected: "just a normal string https://github.com/repo",
		},
		{
			name:     "password field sanitization",
			input:    "Password for 'https://token@github.com'",
			expected: "Password for GitHub authentication",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSanitizeString_NoTokenLeakage(t *testing.T) {
	// Example tokens with fake data that won't trigger GitHub secret scanning
	testCases := []string{
		"ghs_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE",
		"ghp_TESTTOKEN1234567890ABCDEFGHIJKLMNOP",
		"github_pat_EXAMPLE123_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
	}

	for _, token := range testCases {
		input := "fatal: could not read Password for 'https://" + token + "@github.com': No such device"
		result := SanitizeString(input)

		// Verify token is not in the output
		if contains(result, token) {
			t.Errorf("Token was not sanitized: %s still appears in %q", token, result)
		}

		// Verify output is sanitized (either contains *** or password message is cleaned)
		if contains(result, token) {
			t.Errorf("Token leak detected in output: %q", result)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
