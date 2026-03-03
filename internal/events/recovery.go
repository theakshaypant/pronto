package events

import (
	"fmt"
	"log"
	"runtime/debug"
)

// RecoverFromPanic recovers from panics and logs the stack trace.
// This ensures the action doesn't crash silently.
func RecoverFromPanic() {
	if r := recover(); r != nil {
		log.Printf("PANIC RECOVERED: %v", r)
		log.Printf("Stack trace:\n%s", debug.Stack())
	}
}

// SafeProcess wraps the Process method with panic recovery.
func (p *PRProcessor) SafeProcess(action EventAction) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in ProcessEvent: %v", r)
			log.Printf("Stack trace:\n%s", debug.Stack())
			err = fmt.Errorf("panic during processing: %v", r)
		}
	}()

	return p.Process(action)
}
