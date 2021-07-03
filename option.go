package loki

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Option is the parameter type for options when initializing the log hook.
type Option interface {
	apply(h *Hook)
}

// WithSource adds the additional label "source" to all log entries sent to loki.
func WithSource(v string) Option {
	return srcAttrOption(v)
}

type srcAttrOption string

func (o srcAttrOption) apply(h *Hook) {
	h.labels["source"] = string(o)
}

// WithLabel adds an extra labels to all log entries sent to loki.
func WithLabel(k string, v interface{}) Option {
	return labelOption{key: k, value: v}
}

type labelOption struct {
	key   string
	value interface{}
}

func (o labelOption) apply(h *Hook) {
	h.labels[o.key] = o.value
}

// WithLabelsEnabled determines the attributes to be added as labels.
func WithLabelsEnabled(v ...Label) Option {
	return labelEnabledOption(v)
}

type labelEnabledOption []Label

func (o labelEnabledOption) apply(h *Hook) {
	h.labelsEnabled = []Label(o)
}

// WithFormatter sets the formatter for the message.
func WithFormatter(v logrus.Formatter) Option {
	return formatterOption{f: v}
}

type formatterOption struct {
	f logrus.Formatter
}

func (o formatterOption) apply(h *Hook) {
	h.formatter = o.f
}

// WithRemoveColors removes colors from the serialized log entry.
func WithRemoveColors(v bool) Option {
	return removeColorsOption(v)
}

type removeColorsOption bool

func (o removeColorsOption) apply(h *Hook) {
	h.removeColors = bool(o)
}

// WithLevel ignores all log entries with a severity below the level.
func WithLevel(v logrus.Level) Option {
	return levelOption(v)
}

type levelOption logrus.Level

func (o levelOption) apply(h *Hook) {
	h.level = logrus.Level(o)
}

// WithBatchInterval sets the interval at which collected logs are sent.
func WithBatchInterval(v time.Duration) Option {
	return batchIntervalOption(v)
}

type batchIntervalOption time.Duration

func (o batchIntervalOption) apply(h *Hook) {
	h.batchInterval = time.Duration(o)
}

// WithBatchSize sets the buffer size at which all collected logs are sent out when exceeded.
func WithBatchSize(v int) Option {
	return batchSizeOption(v)
}

type batchSizeOption int

func (o batchSizeOption) apply(h *Hook) {
	h.batchSize = int(o)
}

// WithSynchronous sets the synchronous or asynchronous mode.
func WithSynchronous(v bool) Option {
	return synchronousOption(v)
}

type synchronousOption bool

func (o synchronousOption) apply(h *Hook) {
	h.synchronous = bool(o)
}

// WithSuppressErrors ignores errors in asynchronous mode.
func WithSuppressErrors(v bool) Option {
	return suppressErrorsOption(v)
}

type suppressErrorsOption bool

func (o suppressErrorsOption) apply(h *Hook) {
	h.suppressErrors = bool(o)
}
