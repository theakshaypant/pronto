package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

// Printer handles formatted terminal output with optional color support.
type Printer struct {
	out     io.Writer
	errOut  io.Writer
	color   bool
	verbose bool
}

// NewPrinter creates a Printer that auto-detects TTY for color support.
func NewPrinter(verbose bool) *Printer {
	isTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	return &Printer{
		out:     os.Stdout,
		errOut:  os.Stderr,
		color:   isTerminal,
		verbose: verbose,
	}
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func (p *Printer) colorize(color, text string) string {
	if !p.color {
		return text
	}
	return color + text + colorReset
}

// Header prints a bold header line.
func (p *Printer) Header(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.out, p.colorize(colorBold, msg))
}

// Success prints a green success message.
func (p *Printer) Success(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.out, p.colorize(colorGreen, msg))
}

// Error prints a red error message.
func (p *Printer) Error(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.errOut, p.colorize(colorRed, msg))
}

// Warn prints a yellow warning message.
func (p *Printer) Warn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.out, p.colorize(colorYellow, msg))
}

// Info prints an informational message.
func (p *Printer) Info(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.out, msg)
}

// Detail prints a dim detail line (only in verbose mode).
func (p *Printer) Detail(format string, args ...interface{}) {
	if !p.verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(p.out, p.colorize(colorDim, "  "+msg))
}

// Step prints a step with a label.
func (p *Printer) Step(label, status string) {
	fmt.Fprintf(p.out, "  %-40s %s\n", label, p.colorize(colorDim, status))
}

// StepDone prints a completed step.
func (p *Printer) StepDone(label string) {
	fmt.Fprintf(p.out, "  %-40s %s\n", label, p.colorize(colorGreen, "[done]"))
}

// StepFail prints a failed step.
func (p *Printer) StepFail(label string) {
	fmt.Fprintf(p.out, "  %-40s %s\n", label, p.colorize(colorRed, "[failed]"))
}

// Spinner displays a simple animated spinner until stop is called.
type Spinner struct {
	mu      sync.Mutex
	message string
	active  bool
	done    chan struct{}
	out     io.Writer
	color   bool
}

// StartSpinner creates and starts a spinner with the given message.
func (p *Printer) StartSpinner(message string) *Spinner {
	s := &Spinner{
		message: message,
		active:  true,
		done:    make(chan struct{}),
		out:     p.out,
		color:   p.color,
	}

	if !p.color {
		// Non-TTY: just print the message without animation
		fmt.Fprintf(p.out, "  %s... ", message)
		return s
	}

	go s.run()
	return s
}

func (s *Spinner) run() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.active {
				fmt.Fprintf(s.out, "\r  %s %s", frames[i%len(frames)], s.message)
				i++
			}
			s.mu.Unlock()
		}
	}
}

// Stop stops the spinner and prints the final status.
func (s *Spinner) Stop(success bool) {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()

	select {
	case <-s.done:
	default:
		close(s.done)
	}

	if s.color {
		status := "\033[32m✓\033[0m"
		if !success {
			status = "\033[31m✗\033[0m"
		}
		fmt.Fprintf(s.out, "\r  %s %s\n", status, s.message)
	} else {
		if success {
			fmt.Fprintln(s.out, "done")
		} else {
			fmt.Fprintln(s.out, "failed")
		}
	}
}

// Table prints a formatted table.
func (p *Printer) Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(p.out, 0, 0, 2, ' ', 0)

	// Header
	headerLine := strings.Join(headers, "\t")
	fmt.Fprintln(w, p.colorize(colorBold, headerLine))

	// Separator
	sep := make([]string, len(headers))
	for i, h := range headers {
		sep[i] = strings.Repeat("─", len(h)+2)
	}
	fmt.Fprintln(w, strings.Join(sep, "\t"))

	// Rows
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()
}
