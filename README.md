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

	defer hook.Flush()
	log.AddHook(hook)

    // ...
}
```

### Example call in Grafana

```
{source=~"my-go-binary"} | logfmt | level =~ "warning|error|fatal|panic"
```

This displays all log entries of the application, with a severity of at least "warning".

## Options

- `WithSourceAttribute(string)`:  
   By default, the source parameter is sent as the "source" label.
   This can be used to change it or disable it altogether (empty string).
- `WithLabels(...Label)`:  
  Send additional attributes of the entry as labels. By default, only the source attribute is sent.  
  Available labels:
  - `SourceLabel`: add source, enabled by default
  - `FieldsLabel`: add all extra fields as labels
  - `TimeLabel`: add the time as a label
  - `LevelLabel`: add the log level as a label
  - `CallerLabel`: add the caller as format `"[file]:[line]:[function]"`
  - `MessageLabel`: add the message as a label
- `WithFormatter(logrus.Formatter)`:  
  By default, the `TextFormatter` without timestamp is used (`&logrus.TextFormatter{DisableTimestamp: true}`).
- `WithRemoveColors(bool)`:  
  Remove ANSI colors from the serialized log entry.
- `WithMinLevel(logrus.Level)`:  
  Minimum level for log entries
- `WithBatchInterval(time.Duration)`:  
  Batch interval; if this interval is exceeded, all log entries collected so far will be sent out (default: 10 seconds).
- `WithBatchSize(int)`:  
  Maximum batch size; if the number of collected log entries is exceeded, all collected entries will be sent out (default: 1000).
- `WithSynchronous(bool)`:  
  By default, log entries are processed in a separate Go routine. If synchronous sending is used, batch interval and size are ignored.
- `WithSuppressErrors(bool)`:  
  Errors at asynchronous mode are logged to the console. This disables the logging of errors. Ignored at synchronous mode.