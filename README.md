This repository is a mirror of a private GitLab instance. All changes will be overwritten.

# Loki Hook for logrus

This hook allows logging with [logrus](https://github.com/Sirupsen/logrus) to [Loki](https://github.com/grafana/loki).
Logs are sent in [logfmt](https://github.com/kr/logfmt) format.

```
go get github.com/magnetde/loki
```

## Example

```golang
package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/magnetde/loki"
)

func main() {
	// NewHook(source, url, ...options)
	hook, err := loki.NewHook("http://localhost:3200", loki.WithSource("my-go-binary"), loki.WithMinLevel(log.InfoLevel))
	if err != nil {
		// ...
	}

	defer hook.Close()
	log.AddHook(hook)

	// ...
}
```

### Example call in Grafana

```
{source=~"my-go-binary"} | logfmt | level =~ "warning|error|fatal|panic"
```

This displays all log entries of the application, with a severity of at least "warning".
The call depends on the `Formatter`.

## Options

- `WithSource(string)`:  
  This adds the additional label "source" to all log entries sent to loki.
- `WithLabel(string, interface{})`:  
  Similar to `WithSource`, this adds an additional label to all log entries sent to loki.
- `WithLabelsEnabled(...Label)`:  
  Send additional attributes of the entry as labels. By default, only the source attribute is sent.  
  Available labels:
  - labels added with `WithSource` or `WithLabel` are always added to log entires
  - `FieldsLabel`: add all extra fields as labels (`Entry.Data`)
  - `TimeLabel`: add the time as a label (`Entry.Time`)
  - `LevelLabel`: add the log level as a label (`Entry.Level`)
  - `CallerLabel`: add the caller with format `"[file]:[line]:[function]"` (`Entry.Caller`)
  - `MessageLabel`: add the message as a label (`Entry.Message`)
- `WithFormatter(logrus.Formatter)`:  
  By default, the `TextFormatter` with disabled colors and disabled timestamp is used.
  Additional, it formats the caller in format `("file:line:func()", "")` (empty file value).
- `WithRemoveColors(bool)`:  
  Remove ANSI colors from the serialized log entry.
- `WithMinLevel(logrus.Level)`:  
  Minimum level for log entries. All entries that have a lower severity are ignored.
- `WithBatchInterval(time.Duration)`:  
  Batch interval. If this interval has been reached since the last sending, all log entries collected so far will be sent (default: 10 seconds).
- `WithBatchSize(int)`:  
  Maximum batch size. If the number of collected log entries is exceeded, all collected entries will be sent (default: 1000).
- `WithSynchronous(bool)`:  
  By default (async mode), log entries are processed in a separate Go routine. If synchronous sending is used, batch interval and size are ignored.
- `WithSuppressErrors(bool)`:  
  Errors at asynchronous mode are logged to the console. This disables the logging of errors. Ignored at synchronous mode.