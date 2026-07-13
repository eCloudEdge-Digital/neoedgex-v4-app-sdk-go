package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/eCloudEdge-Digital/neoedgex-v4-app-sdk-go/v2/neoedgex/contract"
)

type LoggerImpl struct {
	tag    string
	logger slog.Logger
}

var _ contract.Logger = (*LoggerImpl)(nil)

func NewLogger(tag string) *LoggerImpl {
	var logLevel slog.Level
	if err := (&logLevel).UnmarshalText([]byte(os.Getenv("NEOEDGEX_LOG_LEVEL"))); err != nil {
		// logLevel = slog.LevelInfo
		logLevel = slog.LevelDebug
	}

	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	loggerInstance := slog.New(logHandler)
	return &LoggerImpl{tag: tag, logger: *loggerInstance}
}

func (logger *LoggerImpl) log(level slog.Level, message string, args ...any) {
	logger.logger.Log(context.Background(), level, "["+logger.tag+"] "+fmt.Sprintf(message, args...))
}

func (logger *LoggerImpl) Debug(message string, args ...any) {
	logger.log(slog.LevelDebug, message, args...)
}
func (logger *LoggerImpl) Info(message string, args ...any) {
	logger.log(slog.LevelInfo, message, args...)
}
func (logger *LoggerImpl) Warn(message string, args ...any) {
	logger.log(slog.LevelWarn, message, args...)
}
func (logger *LoggerImpl) Error(message string, args ...any) {
	logger.log(slog.LevelError, message, args...)
}

type noopLogger struct{}

var _ contract.Logger = (*noopLogger)(nil)

func NewNoopLogger() contract.Logger  { return &noopLogger{} }
func (n *noopLogger) Debug(string, ...any) {}
func (n *noopLogger) Info(string, ...any)  {}
func (n *noopLogger) Warn(string, ...any)  {}
func (n *noopLogger) Error(string, ...any) {}
