// Package logx 提供结构化日志接口，基于 slog 实现。
//
// 所有日志自动包含调用方的文件名和行号（短路径）。
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Logger 日志接口，便于替换实现或 mock 测试。
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// Close 关闭日志输出（刷新缓冲区、关闭文件等）。仅当 Logger 拥有资源时需要。
	Close() error
}

// slogger slog 适配器。
// 所有方法通过 runtime.Caller 获取实际调用方的文件名:行号。
type slogger struct {
	inner *slog.Logger
	file  *os.File // NewWithFile 时持有的文件句柄
}

func (l *slogger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

func (l *slogger) log(level slog.Level, msg string, args ...any) {
	if !l.inner.Enabled(nil, level) {
		return
	}
	var pcs [1]uintptr
	// skip: runtime.Callers → log → Debug/Info/Warn/Error → caller
	runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:])
	frame, _ := frames.Next()
	// 只保留文件名（相对路径的最后两级：pkg/file.go 或 file.go）
	file := frame.File
	if len(file) > 0 {
		// 取最后两段，如 "service/service.go" 或仅文件名
		if dir, fname := filepath.Split(file); dir != "" {
			if pdir := filepath.Base(filepath.Dir(dir)); pdir != "" && pdir != "." {
				file = filepath.Join(filepath.Base(dir), fname)
			} else {
				file = fname
			}
		}
	}
	r := slog.Record{
		Time: time.Now(),
		Level: level,
		Message: msg,
	}
	r.AddAttrs(slog.String("source", file+":"+fntoa(frame.Line)))
	r.Add(args...)
	_ = l.inner.Handler().Handle(nil, r)
}

func (l *slogger) Debug(msg string, args ...any) { l.log(slog.LevelDebug, msg, args...) }
func (l *slogger) Info(msg string, args ...any)  { l.log(slog.LevelInfo, msg, args...) }
func (l *slogger) Warn(msg string, args ...any)  { l.log(slog.LevelWarn, msg, args...) }
func (l *slogger) Error(msg string, args ...any) { l.log(slog.LevelError, msg, args...) }

func fntoa(n int) string {
	if n == 0 {
		return ""
	}
	var b [20]byte
	i := len(b)
	for n >= 10 || n <= -10 {
		i--
		q := n / 10
		b[i] = byte('0' + abs(n-q*10))
		n = q
	}
	i--
	b[i] = byte('0' + abs(n))
	if n < 0 {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// newHandler 创建不带 AddSource 的 JSON handler（我们手动记录 source）。
func newHandler(w io.Writer, level slog.Level) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		AddSource: false,
		Level:     level,
	})
}

// New 创建 Logger。输出到 stderr。
func New() Logger {
	handler := newHandler(os.Stderr, slog.LevelDebug)
	return &slogger{inner: slog.New(handler)}
}

// NewWithFile 创建 Logger。同时输出到 stderr 和指定文件。
// level 为日志级别: "debug"/"info"/"warn"/"error"，空默认 "info"。
func NewWithFile(path, level string) (Logger, error) {
	var f *os.File
	var err error
	if path != "" {
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
	}
	writer := io.MultiWriter(os.Stderr)
	if f != nil {
		writer = io.MultiWriter(os.Stderr, f)
	}
	slogLevel := parseLevel(level)
	handler := newHandler(writer, slogLevel)
	return &slogger{inner: slog.New(handler), file: f}, nil
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Nop 返回一个丢弃所有日志的 Logger（用于测试）。
func Nop() Logger {
	return &slogger{inner: slog.New(newHandler(io.Discard, slog.LevelDebug))}
}
