package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
	_ "unsafe"
)

//go:linkname IgnorePC log/slog/internal.IgnorePC
var IgnorePC = true

func BenchmarkSpy(b *testing.B) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
	spy := NewSpy(handler)
	configs := []struct {
		spy          *Spy
		active       bool
		ignorePC     bool
		handlerDebug bool
	}{
		{spy, true, false, false},
		{spy, true, true, false},
		{spy, false, false, false},
		{spy, false, true, false},
		{nil, false, false, false},
		{nil, false, false, true},
		{nil, false, true, false},
		{nil, false, true, true},
	}

	for _, config := range configs {
		spyDesc := "no spy"

		if config.spy != nil {
			spyDesc = "active spy"
			if !config.active {
				spyDesc = "inactive spy"
			}
		}

		desc := fmt.Sprintf("%s ignorePC=%t", spyDesc, config.ignorePC)

		if config.handlerDebug {
			desc += " mainLevel=debug"
		}

		b.Run(desc, func(b *testing.B) {
			if config.handlerDebug {
				handlerBuf := &bytes.Buffer{}
				handler = slog.NewTextHandler(handlerBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
			}

			var h slog.Handler = handler

			IgnorePC = config.ignorePC

			if config.spy != nil {
				spy := config.spy
				go spy.Run(func(msg []byte) {
					// immitate some work
					time.Sleep(10 * time.Millisecond)
				})
				defer spy.Shutdown(context.Background())

				if config.active {
					spy.Watch()
					defer spy.Unwatch()
				}

				h = spy
			}

			logger := slog.New(h)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				logger.Debug("test", "key", 1, "key2", "value2", "key3", 3.14)
			}
		})
	}
}

func TestSpy__Handle(t *testing.T) {
	mainBuf := &bytes.Buffer{}
	buf := &bytes.Buffer{}

	done := make(chan struct{})

	output := func(msg []byte) {
		buf.Write(msg)

		if bytes.Contains(msg, []byte("done")) {
			close(done)
		}
	}

	handler := slog.NewTextHandler(mainBuf, &slog.HandlerOptions{Level: slog.LevelInfo})
	spy := NewSpy(handler)

	logger := slog.New(spy)

	go spy.Run(output)
	defer spy.Shutdown(context.Background())

	logger.Debug("never")
	logger.Info("only-main")

	spy.Watch()
	logger.Debug("only-spy")

	spy.Watch()
	logger.Info("both")

	spy.Unwatch()
	logger.Debug("still-spying")

	spy.Unwatch()
	logger.Debug("never-again")

	spy.Watch()
	logger.Debug("done")

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out to receive done message")
	}

	assertBufferContains(t, mainBuf, "only-main")
	assertBufferContains(t, buf, "only-spy")
	assertBufferContains(t, mainBuf, "both")
	assertBufferContains(t, buf, "both")
	assertBufferContains(t, buf, "still-spying")
	assertBufferContainsNot(t, buf, "never")
}

func assertBufferContains(t *testing.T, buf *bytes.Buffer, expected string) {
	t.Helper()

	if !bytes.Contains(buf.Bytes(), []byte(expected)) {
		t.Errorf("expected buffer to contain %s, got %s", expected, buf.String())
	}
}

func assertBufferContainsNot(t *testing.T, buf *bytes.Buffer, expected string) {
	t.Helper()

	if bytes.Contains(buf.Bytes(), []byte(expected)) {
		t.Errorf("expected buffer to not contain %s, got %s", expected, buf.String())
	}
}
