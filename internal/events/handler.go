package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/go-github/v81/github"
)

// EventType represents the type of GitHub event.
type EventType string

const (
	EventTypePullRequest EventType = "pull_request"
)

// EventAction represents the action within an event.
type EventAction string

const (
	EventActionLabeled EventAction = "labeled"
	EventActionClosed  EventAction = "closed"
)

// Handler processes GitHub webhook events.
type Handler struct{}

// NewHandler creates a new event handler.
func NewHandler() *Handler {
	return &Handler{}
}

// ParseEvent reads and parses the GitHub event payload from GITHUB_EVENT_PATH.
func (h *Handler) ParseEvent(ctx context.Context) (*github.PullRequestEvent, EventType, EventAction, error) {
	// Get event path from environment
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return nil, "", "", fmt.Errorf("GITHUB_EVENT_PATH environment variable not set")
	}

	// Read event file
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read event file: %w", err)
	}

	// Get event type from environment
	eventType := EventType(os.Getenv("GITHUB_EVENT_NAME"))
	if eventType == "" {
		return nil, "", "", fmt.Errorf("GITHUB_EVENT_NAME environment variable not set")
	}

	// Only handle pull_request events
	if eventType != EventTypePullRequest {
		log.Printf("Event type %s is not supported, skipping", eventType)
		return nil, eventType, "", nil
	}

	// Parse pull request event
	var prEvent github.PullRequestEvent
	if err := json.Unmarshal(eventData, &prEvent); err != nil {
		return nil, "", "", fmt.Errorf("failed to parse pull request event: %w", err)
	}

	// Get action
	action := EventAction("")
	if prEvent.Action != nil {
		action = EventAction(*prEvent.Action)
	}

	return &prEvent, eventType, action, nil
}

// ShouldProcess determines if the event should be processed based on type and action.
func (h *Handler) ShouldProcess(eventType EventType, action EventAction) bool {
	// Only process pull_request events
	if eventType != EventTypePullRequest {
		return false
	}

	// Only process labeled and closed actions
	switch action {
	case EventActionLabeled, EventActionClosed:
		return true
	default:
		return false
	}
}

// ValidateEvent performs basic validation on the pull request event.
func (h *Handler) ValidateEvent(event *github.PullRequestEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	if event.PullRequest == nil {
		return fmt.Errorf("pull request cannot be nil")
	}

	if event.Repo == nil {
		return fmt.Errorf("repository cannot be nil")
	}

	if event.PullRequest.Number == nil {
		return fmt.Errorf("pull request number cannot be nil")
	}

	return nil
}
