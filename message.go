package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Label is the enum type to define additional labels to be added to the Loki message.
type Label uint

const (
	// LabelAttrs adds all extra attributes as labels
	LabelAttrs Label = 1 << iota

	// LabelTime adds the time
	LabelTime

	// LabelLevel adds the level
	LabelLevel

	// LabelCaller adds the caller which format "[file]:[line]:[function]"
	LabelCaller

	// LabelMessage adds the message as an extra label
	LabelMessage
)

// LabelAll adds all fields and attributes as labels.
var LabelAll = []Label{LabelAttrs, LabelTime, LabelLevel, LabelCaller, LabelMessage}

// Type representing a single Loki stream element.
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values []*lokiValue      `json:"values"`
}

// Type representing a Loki value.
type lokiValue struct {
	time    time.Time
	message string
}

// Check if the type satisfies the [json.Marshaler] interface.
var _ json.Marshaler = (*lokiValue)(nil)

func (v *lokiValue) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(`["`)
	buf.WriteString(strconv.FormatInt(v.time.UnixNano(), 10))
	buf.WriteString(`",`)
	marshalString(&buf, v.message)
	buf.WriteByte(']')

	return buf.Bytes(), nil
}

// lokiValue transforms the slog record into a Loki value.
func (h *Handler) lokiValue(ctx context.Context, handler slog.Handler, r slog.Record) (*lokiValue, error) {
	if h.defaultHandler {
		if name, ok := h.labels["name"]; ok {
			r = r.Clone()
			r.AddAttrs(slog.Any("name", name))
		}
	}

	msg, err := h.handleMessage(ctx, handler, r)
	if err != nil {
		return nil, err
	}

	v := &lokiValue{
		time:    r.Time,
		message: msg,
	}

	return v, nil
}

func (h *Handler) handleMessage(ctx context.Context, handler slog.Handler, r slog.Record) (string, error) {
	h.buflock.Lock() // lock the buffer
	defer h.buflock.Unlock()

	err := handler.Handle(ctx, r)
	if err != nil {
		return "", err
	}

	// remove trailing new line
	msg := strings.TrimSuffix(h.strbuf.String(), "\n")
	h.strbuf.Reset()

	return msg, nil
}

// lokiLabels returns the list of labels for the slog record.
func (h *Handler) lokiLabels(prefix string, attrs map[string]string, r *slog.Record) map[string]string {
	l := make(map[string]string)
	maps.Copy(l, h.labels)

	for _, lbl := range h.labelsEnabled {
		switch lbl {
		case LabelAttrs:
			maps.Copy(l, attrs)

			r.Attrs(func(a slog.Attr) bool {
				addAttr(l, prefix, a)
				return true
			})
		case LabelTime:
			l["time"] = formatRFC3339Millis(r.Time)
		case LabelLevel:
			l["level"] = r.Level.String()
		case LabelCaller:
			if r.PC != 0 {
				fs := runtime.CallersFrames([]uintptr{r.PC})
				f, _ := fs.Next()
				if f.File != "" {
					function, _ := callerPrettifier(&f)
					l["func"] = function
				}
			}
		case LabelMessage:
			l["msg"] = r.Message
		}
	}

	return l
}

// formatRFC3339Millis formats a time as RFC3339 with millisecond precision.
func formatRFC3339Millis(t time.Time) string {
	// Format according to time.RFC3339Nano since it is highly optimized,
	// but truncate it to use millisecond resolution.
	// Unfortunately, that format trims trailing 0s, so add 1/10 millisecond
	// to guarantee that there are exactly 4 digits after the period.
	const prefixLen = len("2006-01-02T15:04:05.000")

	var b []byte

	t = t.Truncate(time.Millisecond).Add(time.Millisecond / 10)
	b = t.AppendFormat(b, time.RFC3339Nano)
	b = append(b[:prefixLen], b[prefixLen+1:]...) // drop the 4th digit

	return string(b)
}

// callerPrettifier formats a func value in format ("file:line:func", "") (empty file value).
// This kind of creating the caller string is the most efficient way, e.g. compared to Sprintf.
// - this implementation: 258 ns/op, 67 B/op
// - Sprintf:             794 ns/op, 112 B/op
func callerPrettifier(c *runtime.Frame) (string, string) {
	i := strings.LastIndexByte(c.Function, '.')

	var f strings.Builder
	f.Grow(len(c.File) + (len(c.Function) - i) + 10) // 10 extra needed

	f.WriteString(c.File)
	f.WriteByte(':')
	f.WriteString(strconv.Itoa(c.Line))
	f.WriteByte(':')

	if i >= 0 {
		f.WriteString(c.Function[i+1:])
	} else {
		f.WriteString(c.Function)
	}

	f.WriteString("()")
	return f.String(), ""
}
