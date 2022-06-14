package stomp

import "fmt"

var (
	// NilLogger is an empty or no-op logger.
	NilLogger = Logger((*nilLogger)(nil))

	// StdoutLogger logs to standard output.
	StdoutLogger = Logger(stdoutLogger{})
)

// Logger is the interface require for logging.
type Logger interface {
	// Infof logs an informational message using a fmt.Sprintf syntax.
	Infof(fmt string, args ...interface{})
}

type nilLogger struct{}

func (l *nilLogger) Infof(f string, args ...interface{}) {}

type stdoutLogger struct{}

func (l stdoutLogger) Infof(f string, args ...interface{}) {
	fmt.Printf(f+"\n", args...)
}
