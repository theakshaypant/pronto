package events

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParsePRNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "single PR",
			input:    "Cherry-pick #123",
			expected: []int{123},
		},
		{
			name:     "multiple PRs with commas",
			input:    "Cherry-pick #123, #456, #789",
			expected: []int{123, 456, 789},
		},
		{
			name:     "multiple PRs with 'and'",
			input:    "Cherry-pick #123 and #456",
			expected: []int{123, 456},
		},
		{
			name:     "PRs in list format",
			input:    "- #123\n- #456\n- #789",
			expected: []int{123, 456, 789},
		},
		{
			name:     "duplicate PRs",
			input:    "Cherry-pick #123, #456, #123",
			expected: []int{123, 456},
		},
		{
			name:     "no PRs",
			input:    "No PR numbers here",
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "mixed format",
			input:    "Backport #123 to release\n\nAlso #456 and #789",
			expected: []int{123, 456, 789},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePRNumbers(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParsePRNumbers() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilterMergedPRs(t *testing.T) {
	tests := []struct {
		name     string
		results  []PRValidationResult
		expected []int
	}{
		{
			name: "all merged",
			results: []PRValidationResult{
				{Number: 123, Exists: true, Merged: true},
				{Number: 456, Exists: true, Merged: true},
			},
			expected: []int{123, 456},
		},
		{
			name: "mixed merged and unmerged",
			results: []PRValidationResult{
				{Number: 123, Exists: true, Merged: true},
				{Number: 456, Exists: true, Merged: false},
				{Number: 789, Exists: true, Merged: true},
			},
			expected: []int{123, 789},
		},
		{
			name: "none merged",
			results: []PRValidationResult{
				{Number: 123, Exists: true, Merged: false},
				{Number: 456, Exists: true, Merged: false},
			},
			expected: nil,
		},
		{
			name: "non-existent PRs",
			results: []PRValidationResult{
				{Number: 123, Exists: false, Merged: false},
				{Number: 456, Exists: true, Merged: true},
			},
			expected: []int{456},
		},
		{
			name: "PRs with errors",
			results: []PRValidationResult{
				{Number: 123, Exists: true, Merged: true},
				{Number: 456, Exists: true, Merged: true, Error: fmt.Errorf("API error")},
			},
			expected: []int{123},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterMergedPRs(tt.results)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("FilterMergedPRs() = %v, want %v", result, tt.expected)
			}
		})
	}
}
