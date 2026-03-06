package git

import (
	"regexp"
	"strings"
)

// tokenPatterns are regex patterns that match GitHub tokens in URLs and error messages
var tokenPatterns = []*regexp.Regexp{
	// GitHub tokens in URLs: https://TOKEN@github.com or https://x-access-token:TOKEN@github.com
	regexp.MustCompile(`https://x-access-token:[a-zA-Z0-9_-]+@`),
	regexp.MustCompile(`https://[a-zA-Z0-9_-]+@`),
	// Standalone GitHub tokens (ghs_, ghp_, github_pat_)
	// Tokens are typically 36+ chars but we match 16+ to be safe
	regexp.MustCompile(`\b(ghs|ghp)_[a-zA-Z0-9]{16,}\b`),
	regexp.MustCompile(`\bgithub_pat_[a-zA-Z0-9_]{22,}\b`),
}

// SanitizeError removes sensitive information (tokens) from error messages.
// This prevents tokens from being leaked in comments, logs, or PR descriptions.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}

	return SanitizeString(err.Error())
}

// SanitizeString removes sensitive information (tokens) from any string.
func SanitizeString(s string) string {
	sanitized := s

	// Replace x-access-token URLs first (more specific pattern)
	sanitized = tokenPatterns[0].ReplaceAllString(sanitized, "https://x-access-token:***@")

	// Replace other token URLs
	sanitized = tokenPatterns[1].ReplaceAllString(sanitized, "https://***@")

	// Replace standalone tokens
	for i := 2; i < len(tokenPatterns); i++ {
		sanitized = tokenPatterns[i].ReplaceAllString(sanitized, "***")
	}

	// Also sanitize any password fields in git output
	if strings.Contains(sanitized, "Password for") {
		// Replace "Password for 'https://***@github.com'" with cleaner message
		sanitized = regexp.MustCompile(`Password for '[^']*'`).ReplaceAllString(
			sanitized,
			"Password for GitHub authentication",
		)
	}

	return sanitized
}
