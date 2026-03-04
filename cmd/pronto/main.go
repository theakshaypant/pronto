package main

import (
	"context"
	"fmt"
	"log"

	"github.com/google/go-github/v81/github"
	"github.com/theakshaypant/pronto/internal/action"
	"github.com/theakshaypant/pronto/internal/events"
)

func main() {
	// Run the action and handle any errors
	if err := run(); err != nil {
		log.Fatalf("PROnto action failed: %v", err)
	}

	log.Println("PROnto action completed successfully")
}

func run() error {
	ctx := context.Background()

	// Load configuration
	log.Println("Loading configuration...")
	cfg, err := action.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	log.Println("Configuration loaded successfully")
	log.Printf("Label pattern: %s", cfg.LabelPattern)
	log.Printf("Conflict label: %s", cfg.ConflictLabel)
	log.Printf("Bot name: %s", cfg.BotName)

	// Create event handler
	handler := events.NewHandler()

	// Parse the GitHub event
	log.Println("Parsing GitHub event...")
	event, eventType, eventAction, err := handler.ParseEvent(ctx)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Check if event should be processed
	if !handler.ShouldProcess(eventType, eventAction) {
		log.Printf("Event type '%s' with action '%s' does not require processing, skipping", eventType, eventAction)
		return nil
	}

	// Route to appropriate processor based on event type
	switch eventType {
	case events.EventTypePullRequest:
		return processPullRequest(ctx, handler, cfg, event, eventAction)

	case events.EventTypeIssues:
		return processIssue(ctx, handler, cfg, event, eventAction)

	default:
		return fmt.Errorf("unsupported event type: %s", eventType)
	}
}

// processPullRequest handles pull request events.
func processPullRequest(ctx context.Context, handler *events.Handler, cfg *action.Config, event interface{}, eventAction events.EventAction) error {
	// Type assert to pull request event
	prEvent, ok := event.(*github.PullRequestEvent)
	if !ok {
		return fmt.Errorf("invalid pull request event type")
	}

	// Validate event
	if err := handler.ValidateEvent(prEvent); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	log.Printf("Processing pull request #%d (action: %s)", *prEvent.PullRequest.Number, eventAction)

	// Create PR processor
	processor, err := events.NewPRProcessor(ctx, cfg, prEvent)
	if err != nil {
		return fmt.Errorf("failed to create PR processor: %w", err)
	}

	// Process the pull request event with panic recovery
	if err := processor.SafeProcess(eventAction); err != nil {
		return fmt.Errorf("failed to process pull request: %w", err)
	}

	return nil
}

// processIssue handles issue events.
func processIssue(ctx context.Context, handler *events.Handler, cfg *action.Config, event interface{}, eventAction events.EventAction) error {
	// Type assert to issues event
	issueEvent, ok := event.(*github.IssuesEvent)
	if !ok {
		return fmt.Errorf("invalid issues event type")
	}

	// Validate event
	if err := handler.ValidateIssueEvent(issueEvent); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	log.Printf("Processing issue #%d (action: %s)", *issueEvent.Issue.Number, eventAction)

	// Create issue processor
	processor, err := events.NewIssueProcessor(ctx, cfg, issueEvent)
	if err != nil {
		return fmt.Errorf("failed to create issue processor: %w", err)
	}

	// Process the issue event with panic recovery
	if err := processor.SafeProcess(eventAction); err != nil {
		return fmt.Errorf("failed to process issue: %w", err)
	}

	return nil
}
