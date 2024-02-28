This repository is a mirror of a private GitLab instance.
Note that any changes will be overwritten.

# Loki Handler for slog

This handler allows logging with [slog](https://pkg.go.dev/log/slog) to [Loki](https://github.com/grafana/loki).
By default, logs are sent asynchronously in batches and are formatted using `slog.TextHandler` (logfmt).

## Installation

```bash
go get github.com/magnetde/slog-loki
```

## Example Usage in Go

```go
package main

import (
	"log/slog"
	loki "github.com/magnetde/slog-loki"
)

func main() {
	// NewHandler(url, ...options)
	handler := loki.NewHandler("http://localhost:3200", loki.WithName("my-go-binary"))
	defer handler.Close()

	slog.SetDefault(slog.New(handler))

	// ...
}
```

The package [slog-multi](https://github.com/samber/slog-multi) is recommended for simultaneous logging to the console and Loki.

### Example Usage in Grafana

```
{name="my-go-binary"} | logfmt | level =~ "WARN|ERROR"
```

This query displays all log entries of the application with a severity of at least "warning".
The format depends on the handler set with `WithHandler`.

### Options

- `WithName(string)`:  
  Adds the additional label "name" to all log entries sent to Loki.
  If the default handler is used, it is also added as an attribute to the formatted log message.
- `WithLabel(string, any)`:  
  Similar to `WithName`, this adds an additional label to all log entries sent to Loki.
- `WithLabelsEnabled(...Label)`:  
  Sends additional labels from log entry attributes. By default, only the name attribute is sent. Available labels include:
  - Labels added with `WithName` or `WithLabel` are always added to log entries.
  - `LabelTime`: Adds the time as a label.
  - `LabelLevel`: Adds the log level as a label.
  - `LabelMessage`: Adds the message as a label.
  - `LabelCaller`: Adds the caller with format `"[file]:[line]:[function]"`.
  - `LabelAttrs`: Adds all extra attributes as labels.
  - `LabelAll`: Adds all fields and attributes as labels.
- `WithHandler(func(w io.Writer) slog.Handler)`:  
  Allows setting a handler to change the format of log entries. The handler should write the log entries to the writer `w`.
  For example, to create log entries in JSON format:
  ```go
  WithHandler(func(w io.Writer) slog.Handler {
      return slog.NewJSONHandler(w, &slog.HandlerOptions{
          // ...
      })
  })
  ```
- `WithSynchronous(bool)`:  
  By default (async mode), log entries are processed in a separate Go routine. If synchronous sending is used, batch interval and size are ignored.
- `WithBatchInterval(time.Duration)`:  
  Sets the batch interval. Every batch interval, all collected log entries will be dispatched (default: 15 seconds).
- `WithBatchSize(int)`:  
  Maximum batch size. If the number of collected log entries exceeds this size, all collected entries will be sent to Loki (default: 1024).
- `WithErrorHandler(bool)`:  
  Sets the error handler for send errors in asynchronous mode. This is ignored in synchronous mode.