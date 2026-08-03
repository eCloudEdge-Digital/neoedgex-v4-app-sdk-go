package contract

// Logger is the logging interface the SDK hands to node handlers, and the
// interface to satisfy when supplying a logger of your own.
//
// msg and args are printf arguments, not slog-style key/value pairs: an
// implementation is expected to render the line as fmt.Sprintf(msg, args...).
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}
