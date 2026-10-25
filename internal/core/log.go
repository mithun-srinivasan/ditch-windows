package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger is the structured logger used by the core and modules. Every logger
// writes one JSON object per line when backed by a file.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	WithContext(context string) Logger
	Path() string
	Close() error
}

type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Context string `json:"context,omitempty"`
	Message string `json:"message"`
}

type fileLogger struct {
	mu      sync.Mutex
	w       io.Writer
	closer  io.Closer
	path    string
	context string
	closed  bool
}

// NewFileLogger opens path for append and returns a JSON-lines logger.
func NewFileLogger(path string) (Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return &fileLogger{w: f, closer: f, path: path}, nil
}

// NewDailyFileLogger creates a logger in dir named by today's UTC date.
func NewDailyFileLogger(dir string) (Logger, error) {
	name := "ditch-windows-" + time.Now().UTC().Format("20060102") + ".log"
	return NewFileLogger(filepath.Join(dir, name))
}

// NewWriterLogger returns a JSON-lines logger for tests or console output.
func NewWriterLogger(w io.Writer) Logger {
	return &fileLogger{w: w}
}

// NopLogger discards all messages.
func NopLogger() Logger { return &fileLogger{w: io.Discard} }

func (l *fileLogger) log(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	entry := logEntry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Level:   level,
		Context: l.context,
		Message: msg,
	}
	enc := json.NewEncoder(l.w)
	_ = enc.Encode(entry)
	if f, ok := l.w.(*os.File); ok {
		_ = f.Sync()
	}
}

func (l *fileLogger) Debugf(format string, args ...any) { l.log("DEBUG", fmt.Sprintf(format, args...)) }
func (l *fileLogger) Infof(format string, args ...any)  { l.log("INFO", fmt.Sprintf(format, args...)) }
func (l *fileLogger) Warnf(format string, args ...any)  { l.log("WARN", fmt.Sprintf(format, args...)) }
func (l *fileLogger) Errorf(format string, args ...any) { l.log("ERROR", fmt.Sprintf(format, args...)) }

func (l *fileLogger) WithContext(context string) Logger {
	return &fileLogger{w: l.w, closer: l.closer, path: l.path, context: context}
}

func (l *fileLogger) Path() string { return l.path }

func (l *fileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// MultiLogger fans out to several loggers.
func MultiLogger(loggers ...Logger) Logger {
	filtered := make([]Logger, 0, len(loggers))
	for _, l := range loggers {
		if l != nil {
			filtered = append(filtered, l)
		}
	}
	return &multiLogger{loggers: filtered}
}

type multiLogger struct{ loggers []Logger }

func (m *multiLogger) Debugf(format string, args ...any) {
	for _, l := range m.loggers {
		l.Debugf(format, args...)
	}
}
func (m *multiLogger) Infof(format string, args ...any) {
	for _, l := range m.loggers {
		l.Infof(format, args...)
	}
}
func (m *multiLogger) Warnf(format string, args ...any) {
	for _, l := range m.loggers {
		l.Warnf(format, args...)
	}
}
func (m *multiLogger) Errorf(format string, args ...any) {
	for _, l := range m.loggers {
		l.Errorf(format, args...)
	}
}
func (m *multiLogger) WithContext(context string) Logger {
	out := make([]Logger, 0, len(m.loggers))
	for _, l := range m.loggers {
		out = append(out, l.WithContext(context))
	}
	return &multiLogger{loggers: out}
}
func (m *multiLogger) Path() string {
	for _, l := range m.loggers {
		if p := l.Path(); p != "" {
			return p
		}
	}
	return ""
}
func (m *multiLogger) Close() error {
	var first error
	for _, l := range m.loggers {
		if err := l.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
