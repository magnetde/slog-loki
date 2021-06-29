## ! Still in development

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
	hook, err := loki.NewHook("my-go-binary", "http://localhost:3200", loki.WithMinLevel(log.InfoLevel))
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

- `WithSourceAttribute(string)`:  
   By default, the source parameter is sent as the label `"source"`.
   This can be used to change it or disable this label (empty string).
- `WithLabels(...Label)`:  
  Send additional attributes of the entry as labels. By default, only the source attribute is sent.  
  Available labels:
  - `SourceLabel`: add the source attribute which is passed at the `NewHook` function; enabled by default
  - `FieldsLabel`: add all extra fields as labels (`Entry.Data`)
  - `TimeLabel`: add the time as a label (`Entry.Time`)
  - `LevelLabel`: add the log level as a label (`Entry.Level`)
  - `CallerLabel`: add the caller with format `"[file]:[line]:[function]"` (`Entry.Caller`)
  - `MessageLabel`: add the message as a label (`Entry.Message`)
- `WithFormatter(logrus.Formatter)`:  
  By default, the `TextFormatter` with disabled timestamp is used (`&logrus.TextFormatter{DisableTimestamp: true}`).
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