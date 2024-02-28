package loki

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"regexp"
	"runtime"
	"sync"
)

// LogfmtHandler is a [slog.Handler] that writes Records as logfmt strings.
type LogfmtHandler struct {
	w      io.Writer
	opts   *LogfmtOptions
	prefix string
	attrs  string
}

// LogfmtOptions are options for a [LogfmtHandler].
type LogfmtOptions struct {
	// AddSource causes the handler to compute the source code position
	// of the log statement and add a SourceKey attribute to the output.
	AddSource bool

	// Level reports the minimum record level that will be logged.
	// The handler discards records with lower levels.
	// If Level is nil, the handler assumes LevelInfo.
	Level slog.Leveler

	// RemoveColors causes the handler to strip ANSI color from the log
	// message and all log attributes.
	RemoveColors bool

	// TimeFormat changes the time format of the time field.
	// If no time format is present, RFC3339 with millisecond precision
	// is used.
	TimeFormat string
}

// NewLogfmtHandler creates a [LogfmtHandler] that writes to `w`, using the given options.
// If opts is nil, the default options are used.
func NewLogfmtHandler(w io.Writer, opts *LogfmtOptions) *LogfmtHandler {
	if opts == nil {
		opts = &LogfmtOptions{}
	}

	return &LogfmtHandler{
		w:      w,
		opts:   opts,
		prefix: "",
		attrs:  "",
	}
}

// Enabled implements the [slog.Handler.Enabled] function.
func (h *LogfmtHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}

	return lvl >= minLevel
}

// Handle implements the [slog.Handler.Handle] function.
func (h *LogfmtHandler) Handle(_ context.Context, r slog.Record) error {
	var b bytes.Buffer

	if layout := h.opts.TimeFormat; layout != "" {
		h.appendKeyValue(&b, slog.TimeKey, r.Time.Format(layout))
	} else {
		h.appendKeyValue(&b, slog.TimeKey, formatRFC3339Millis(r.Time))
	}

	h.appendKeyValue(&b, slog.LevelKey, r.Level.String())
	h.appendKeyValue(&b, slog.MessageKey, r.Message)

	if h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		if f.File != "" {
			function, _ := callerPrettifier(&f)
			h.appendKeyValue(&b, slog.SourceKey, function)
		}
	}

	if len(h.attrs) > 0 {
		b.WriteByte(' ')
		b.WriteString(h.attrs)
	}

	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&b, a)
		return true
	})

	_, err := io.Copy(h.w, &b)
	return err
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *LogfmtHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b bytes.Buffer
	b.WriteString(h.attrs)

	for _, a := range attrs {
		h.appendAttr(&b, a)
	}

	return &LogfmtHandler{
		w:      h.w,
		opts:   h.opts,
		prefix: h.prefix,
		attrs:  b.String(),
	}
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *LogfmtHandler) WithGroup(name string) slog.Handler {
	return &LogfmtHandler{
		w:      h.w,
		opts:   h.opts,
		prefix: h.prefix + name + ".",
		attrs:  h.attrs,
	}
}

// appendAttr unpacks attribute `a` and appends it to buffer `b` in key=val format.
func (h *LogfmtHandler) appendAttr(b *bytes.Buffer, a slog.Attr) {
	unpackAttr(h.prefix, a, func(k, v string) {
		h.appendKeyValue(b, k, v)
	})
}

// appendKeyValue appends the key-value pair to buffer `b` in key=val format.
func (h *LogfmtHandler) appendKeyValue(b *bytes.Buffer, key, value string) {
	if b.Len() > 0 {
		b.WriteByte(' ')
	}

	b.WriteString(key)
	b.WriteByte('=')

	if h.opts.RemoveColors {
		value = removeColors(value)
	}

	appendValue(b, value)
}

// appendValue appends string value `s` to buffer `b`.
// If needed, this function adds quotation marks around the string.
// Quoting is done if either space or '=' occurs.
// Only the characters `"` and `\` are escaped.
func appendValue(b *bytes.Buffer, s string) {
	// check if string needs quoting or escaping
	quoting := len(s) == 0 || s[0] == '"' || s[len(s)-1] == '"'
	escape := false

	// loop to save memory when we do not need to escape
	for _, c := range s {
		switch c {
		case ' ', '=':
			quoting = true
		case '\r', '\n', '\t', '\\', '"':
			escape = true
		}
		if quoting && escape {
			break
		}
	}

	if quoting || escape {
		b.WriteByte('"')
	}
	if escape {
		for _, c := range s {
			switch c {
			case '\r':
				b.WriteString("\\r")
			case '\n':
				b.WriteString("\\n")
			case '\t':
				b.WriteString("\\t")
			case '"', '\\':
				b.WriteByte('\\')
				fallthrough
			default:
				b.WriteRune(c)
			}
		}
	} else {
		b.WriteString(s)
	}
	if quoting || escape {
		b.WriteByte('"')
	}
}

const colors = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[-a-zA-Z\\d\\/#&.:=?%@~_]*)*)?\u0007)" +
	"|" +
	"(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-ntqry=><~]))"

var (
	colorOnce  sync.Once      // ensure, that colorRegexp is singleton
	colorRegex *regexp.Regexp // color regexp to remove ANSI colors
)

// getColorRegexp returns a singleton value of the color regexp.
func getColorRegexp() *regexp.Regexp {
	colorOnce.Do(func() {
		colorRegex = regexp.MustCompile(colors)
	})

	return colorRegex
}

// removeColors removes ANSI-colors in a string
func removeColors(s string) string {
	r := getColorRegexp()

	if r.MatchString(s) {
		return r.ReplaceAllString(s, "")
	}

	return s
}
