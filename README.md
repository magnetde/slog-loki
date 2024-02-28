This repository is a mirror of a private GitLab instance. All changes will be overwritten.

# Loki handler for slog

This handler allows logging with [slog](https://pkg.go.dev/log/slog) to [Loki](https://github.com/grafana/loki).
By default, logs are sent asynchronously in batches and are encoded in the format of `slog.TextHandler` (logfmt).

```
go get github.com/magnetde/loki
```

## Example

```golang
package main

import (
	"log/slog"
	"github.com/magnetde/loki"
)

func main() {
	// NewHandler(url, ...options)
	handler := loki.NewHandler("http://localhost:3200", loki.WithName("my-go-binary"))
	defer handler.Close()

	slog.SetDefault(slog.New(handler))

	// ...
}
```

The package [slog-multi](https://github.com/samber/slog-multi) is recommended to simultaneously log to the console and to Loki.

### Example call in Grafana

```
{name="my-go-binary"} | logfmt | level =~ "warning|error"
```

This displays all log entries of the application, with a severity of at least "warning".
The call depends on the `Formatter`.

## Options

- `WithName(string)`:  
  This adds the additional label "name" to all log entries sent to loki.
  It is the only additional label, that is added to the log message if the default logger is used.
- `WithLabel(string, any)`:  
  Similar to `WithName`, this adds an additional label to all log entries sent to loki.
- `WithLabelsEnabled(...Label)`:  
  Send additional attributes of the entry as labels. By default, only the name attribute is sent.  
  Available labels:
  - labels added with `WithName` or `WithLabel` are always added to log entires
  - `LabelAttrs`: add all extra attributes as labels
  - `LabelTime`: add the time as a label
  - `LabelLevel`: add the log level as a label
  - `LabelCaller`: add the caller with format `"[file]:[line]:[function]"`
  - `LabelMessage`: add the message as a label
  - `LabelAll`: add all fields and attributes as labels
- `WithHandler(func(w io.Writer) slog.Handler)`:  
  The handler can be set to change the format of log entries. In this case, the handler is required to write the log entries to the writer `w`.
  Example for log entries in JSON format:
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
  Batch interval. If this interval has been reached since the last sending, all log entries collected so far will be sent (default: 10 seconds).
- `WithBatchSize(int)`:  
  Maximum batch size. If the number of collected log entries is exceeded, all collected entries will be sent (default: 1000).
- `WithErrorHandler(bool)`:  
  Sets the error handler in asynchronous mode. Ignored at synchronous mode.
