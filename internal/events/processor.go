package events

// EventProcessor defines the interface for processing GitHub events.
// Both PRProcessor and IssueProcessor implement this interface.
type EventProcessor interface {
	// Process handles the event with the given action
	Process(action EventAction) error

	// SafeProcess wraps Process with panic recovery
	SafeProcess(action EventAction) error
}
