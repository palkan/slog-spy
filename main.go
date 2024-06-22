package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	defaultMaxbufSize    = 256 * 1024 // 256KB
	defaultFlushInterval = 250 * time.Millisecond
)

type SpyOutput func(msg []byte)

type SpyCommand int

const (
	SpyCommandRecord SpyCommand = iota
	SpyCommandFlush
	SpyCommandStop
)

type Entry struct {
	record *slog.Record
	// printer keeps the reference to the current printer
	// to carry on log attributes and groups
	printer slog.Handler
	cmd     SpyCommand
}

type SpyHandler struct {
	output SpyOutput

	active *atomic.Int64
	ch     chan *Entry
	timer  *time.Timer
	buf    *bytes.Buffer

	// A log handler we use to format records
	printer       slog.Handler
	maxBufSize    int
	flushInterval time.Duration
}

var _ slog.Handler = (*SpyHandler)(nil)

type SpyHandlerOption func(*SpyHandler)

// WithMaxBufSize sets the maximum output buffer size for the SpyHandler.
func WithMaxBufSize(size int) SpyHandlerOption {
	return func(h *SpyHandler) {
		h.maxBufSize = size
	}
}

// WithFlushInterval sets the max flush interval for the SpyHandler.
func WithFlushInterval(interval time.Duration) SpyHandlerOption {
	return func(h *SpyHandler) {
		h.flushInterval = interval
	}
}

// WithPrinter allows to configure a custom slog.Handler used to format log records.
func WithPrinter(printerBuilder func(io io.Writer) slog.Handler) SpyHandlerOption {
	return func(h *SpyHandler) {
		h.printer = printerBuilder(h.buf)
	}
}

// WithBacklogSize sets the size of the backlog channel used as a queue for log records.
func WithBacklogSize(size int) SpyHandlerOption {
	return func(h *SpyHandler) {
		h.ch = make(chan *Entry, size)
	}
}

// NewSpyHandler creates a new SpyHandler with the provided options.
func NewSpyHandler(opts ...SpyHandlerOption) *SpyHandler {
	buf := &bytes.Buffer{}
	h := &SpyHandler{
		ch:            make(chan *Entry, 2048),
		buf:           buf,
		active:        &atomic.Int64{},
		printer:       slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
		maxBufSize:    defaultMaxbufSize,
		flushInterval: defaultFlushInterval,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

func (h *SpyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.active.Load() > 0
}

func (h *SpyHandler) Handle(ctx context.Context, r slog.Record) error {
	h.enqueueRecord(&r)

	return nil
}

func (h *SpyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := h.Clone()
	newHandler.printer = h.printer.WithAttrs(attrs)
	return newHandler
}

func (h *SpyHandler) WithGroup(name string) slog.Handler {
	newHandler := h.Clone()
	newHandler.printer = newHandler.printer.WithGroup(name)
	return newHandler
}

// Run starts a Go routine which publishes log messages in the background
func (h *SpyHandler) Run(out SpyOutput) {
	h.output = out

	for entry := range h.ch {
		if entry.cmd == SpyCommandStop {
			if h.timer != nil {
				h.timer.Stop()
			}
			return
		}

		if entry.cmd == SpyCommandFlush {
			h.flush()
			continue
		}

		entry.printer.Handle(context.Background(), *entry.record) // nolint: errcheck

		if h.buf.Len() > h.maxBufSize {
			h.flush()
		} else {
			h.resetTimer()
		}
	}
}

func (h *SpyHandler) Shutdown(ctx context.Context) {
	h.ch <- &Entry{cmd: SpyCommandStop}
}

func (h *SpyHandler) Watch() {
	h.active.Add(1)
}

func (h *SpyHandler) Unwatch() {
	h.active.Add(-1)
}

// Clone returns a new SpyHandler with the same parent handler and buffers
func (t *SpyHandler) Clone() *SpyHandler {
	return &SpyHandler{
		output:        t.output,
		active:        t.active,
		ch:            t.ch,
		buf:           t.buf,
		maxBufSize:    t.maxBufSize,
		flushInterval: t.flushInterval,
	}
}

func (h *SpyHandler) enqueueRecord(r *slog.Record) {
	// Make sure we don't block the main thread; it's okay to ignore the record if the channel is full
	select {
	case h.ch <- &Entry{record: r, cmd: SpyCommandRecord, printer: h.printer}:
	default:
	}
}

func (h *SpyHandler) resetTimer() {
	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(h.flushInterval, h.sendFlush)
}

func (h *SpyHandler) sendFlush() {
	h.ch <- &Entry{cmd: SpyCommandFlush}
}

func (h *SpyHandler) flush() {
	if h.buf.Len() == 0 {
		return
	}

	msg := h.buf.Bytes()

	h.output(msg)

	h.buf.Reset()
}

type Spy struct {
	parent  slog.Handler
	handler *SpyHandler
}

var _ slog.Handler = (*Spy)(nil)

func NewSpy(parent slog.Handler, opts ...SpyHandlerOption) *Spy {
	handler := NewSpyHandler(opts...)

	return &Spy{
		parent:  parent,
		handler: handler,
	}
}

func (s *Spy) Enabled(ctx context.Context, level slog.Level) bool {
	if !s.handler.Enabled(ctx, level) {
		return s.parent.Enabled(ctx, level)
	}

	return true
}

func (s *Spy) Handle(ctx context.Context, r slog.Record) (err error) {
	if s.handler.Enabled(ctx, r.Level) {
		s.handler.Handle(ctx, r) // nolint: errcheck
	}

	if s.parent.Enabled(ctx, r.Level) {
		err = s.parent.Handle(ctx, r)
	}

	return
}

func (s *Spy) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Spy{
		parent:  s.parent.WithAttrs(attrs),
		handler: (s.handler.WithAttrs(attrs)).(*SpyHandler),
	}
}

func (s *Spy) WithGroup(name string) slog.Handler {
	return &Spy{
		parent:  s.parent.WithGroup(name),
		handler: (s.handler.WithGroup(name)).(*SpyHandler),
	}
}

func (s *Spy) Handler() slog.Handler {
	return s.parent
}

func (s *Spy) Run(out SpyOutput) {
	s.handler.Run(out)
}

func (s *Spy) Shutdown(ctx context.Context) {
	s.handler.Shutdown(ctx)
}

func (s *Spy) Watch() {
	s.handler.Watch()
}

func (s *Spy) Unwatch() {
	s.handler.Unwatch()
}
