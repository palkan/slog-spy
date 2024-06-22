# slog-spy

Slog handler (or wrapper) to temporary deliver formatted verbose logs to an arbitrary target. Useful for providing diagnostic/troubleshooting logging functionality to your Go application.

> ![NOTE]
> This library has been extracted from [AnyCable](https://github.com/anycable/anycable-go).

## Install

```sh
go get github.com/palkan/slog-spy
```

**Compatibility**: go >= 1.21

## Usage

```go
import (
    slogspy "github.com/palkan/slog-spy"
    "log/slog"
)

func main() {
    handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})

    // Create a spy handling by wrapping the default one
    spy := slogspy.NewSpy(handler)

    // Use it with your logger
    logger := slog.New(spy)

    // Start spy go routine to process logs in the background (when they're requested)
    go spyHandler.Run(myLogsConsumer)
    defer spyHandler.Shutdown(context.Background())

    // your application logic

    // whenever you want to start consuming verbose logs via the spy
    spy.Watch()
    // don't forget to unwatch to disable the spy handler
    defer spy.Unwatch()
}

func myLogsConsumer(logs []byte) {
  // consume pre-formatted logs here
}
```

You MAY call `spy.Watch()` multiple times (indicating that there are multiple consumers); you MUST call `spy.Unwatch()` the same number of times to deactivate the spy. The logs are streamed to the callback function as long as there is at least one consumer.

### Configuration

By default, a spy handler uses a JSON handler to format the logs and produce the raw bytes. The output is buffered (to prevent too frequent consumer function calling). The buffer flushing is controlled by two parameters: max buffer size and flush interval.

Here is how you can adjust all of the parameters mentioned above (with the defaults specified):

```go
spy := slogspy.NewSpy(
  handler,
  slogspy.WithMaxBufSize(256 * 1024),
  slogspy.WithFlushInterval(250 * time.Millisecond),
  slogspy.WithPrinter(func(output io.Writer) slog.Handler {
    return slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})
  }),
)
```

## Benchmarks

The spy handler in the idle state has no noticeable overhead. When it's active, the overhead is ~2x lower than when turning debug logs on for the base handler. Here are the numbers:

```sh
BenchmarkSpy/active_spy                       372.5 ns/op
BenchmarkSpy/inactive_spy                     8.380 ns/op
BenchmarkSpy/no_spy                           7.681 ns/op
BenchmarkSpy/no_spy_mainLevel=debug           656.3 ns/op
```

The source code can be found in the `main_test.go` file.

### IgnorePC optimization

You can improve the performance even more by disabling the caller information retrieval for log records:

```go
//go:linkname IgnorePC log/slog/internal.IgnorePC
var IgnorePC = true
```

## Future enhancements

- Support watching specific key-value attribute pairs (e.g., `spy.WatchAttrs("user_id", "42")`)

## License

This project is [MIT](./MIT-LICENSE) licensed.
