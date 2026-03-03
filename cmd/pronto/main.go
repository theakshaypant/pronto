package main

import (
	"context"
	"fmt"
	"log"

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
	prEvent, eventType, eventAction, err := handler.ParseEvent(ctx)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Check if event should be processed
	if !handler.ShouldProcess(eventType, eventAction) {
		log.Printf("Event type '%s' with action '%s' does not require processing, skipping", eventType, eventAction)
		return nil
	}

	// Validate event
	if err := handler.ValidateEvent(prEvent); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	log.Printf("Processing pull request #%d (action: %s)", *prEvent.PullRequest.Number, eventAction)

	// TODO: Phase 3+ will add actual processing logic here
	// - GitHub client initialization
	// - Permission checking
	// - Label parsing
	// - Cherry-pick operations

	log.Println("WARNING: Core processing not yet implemented - will be added in Phase 3+")

	return nil
}
