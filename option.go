package loki

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"
)

// Option is the parameter type for options when initializing the log handler.
type Option interface {
	apply(h *Handler)
}

// WithName adds the additional label "name" to all log entries sent to loki.
func WithName(v string) Option {
	return labelOption{key: "name", value: v}
}

// WithLabel adds an extra labels to all log entries sent to loki.
func WithLabel(k string, v any) Option {
	return labelOption{key: k, value: v}
}

type labelOption struct {
	key   string
	value any
}

func (o labelOption) apply(h *Handler) {
	if h.labels == nil {
		h.labels = make(map[string]string)
	}

	h.labels[o.key] = fmt.Sprint(o.value)
}

// WithLabelsEnabled determines the attributes to be added as labels.
func WithLabelsEnabled(v ...Label) Option {
	return labelEnabledOption(v)
}

type labelEnabledOption []Label

func (o labelEnabledOption) apply(h *Handler) {
	h.labelsEnabled = append(h.labelsEnabled, o...)

	if !h.labelAttrs {
		h.labelAttrs = slices.Contains(o, LabelAttrs)
	}
}

// WithHandler sets the handler that formats the log entries.
func WithHandler(h func(w io.Writer) slog.Handler) Option {
	return handlerOption(h)
}

type handlerOption func(w io.Writer) slog.Handler

func (o handlerOption) apply(h *Handler) {
	h.handler = o(&h.strbuf)
}

// WithSynchronous sets the synchronous or asynchronous mode.
func WithSynchronous(v bool) Option {
	return synchronousOption(v)
}

type synchronousOption bool

func (o synchronousOption) apply(h *Handler) {
	h.synchronous = bool(o)
}

// WithBatchInterval sets the interval at which collected logs are sent.
func WithBatchInterval(v time.Duration) Option {
	return batchIntervalOption(v)
}

type batchIntervalOption time.Duration

func (o batchIntervalOption) apply(h *Handler) {
	h.batchInterval = time.Duration(o)
}

// WithBatchSize sets the buffer size at which all collected logs are sent out when exceeded.
func WithBatchSize(v int) Option {
	return batchSizeOption(v)
}

type batchSizeOption int

func (o batchSizeOption) apply(h *Handler) {
	h.batchSize = int(o)
}

// WithErrorHandler handles request errors in asynchronous mode.
func WithErrorHandler(v func(err error)) Option {
	return errorHandlerOption(v)
}

type errorHandlerOption func(err error)

func (o errorHandlerOption) apply(h *Handler) {
	h.errHandler = o
}
