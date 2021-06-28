package loki

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Option is the parameter type for options when initializing the log hook.
type Option interface {
	apply(h *Hook)
}

func WithSourceAttribute(v string) Option {
	return srcAttrOption(v)
}

type srcAttrOption string

func (o srcAttrOption) apply(h *Hook) {
	h.srcAttr = string(o)
}

func WithLabels(v ...Label) Option {
	return labelOption(v)
}

type labelOption []Label

func (o labelOption) apply(h *Hook) {
	h.labels = []Label(o)
}

func WithFormatter(v logrus.Formatter) Option {
	return formatterOption{f: v}
}

type formatterOption struct {
	f logrus.Formatter
}

func (o formatterOption) apply(h *Hook) {
	h.formatter = o.f
}

func WithRemoveColors(v bool) Option {
	return removeColorsOption(v)
}

type removeColorsOption bool

func (o removeColorsOption) apply(h *Hook) {
	h.removeColors = bool(o)
}

func WithMinLevel(v logrus.Level) Option {
	return minLevelOption(v)
}

type minLevelOption logrus.Level

func (o minLevelOption) apply(h *Hook) {
	h.minLevel = logrus.Level(o)
}

func WithBatchInterval(v time.Duration) Option {
	return batchIntervalOption(v)
}

type batchIntervalOption time.Duration

func (o batchIntervalOption) apply(h *Hook) {
	h.batchInterval = time.Duration(o)
}

func WithBatchSize(v int) Option {
	return batchSizeOption(v)
}

type batchSizeOption int

func (o batchSizeOption) apply(h *Hook) {
	h.batchSize = int(o)
}

func WithSynchronous(v bool) Option {
	return synchronousOption(v)
}

type synchronousOption bool

func (o synchronousOption) apply(h *Hook) {
	h.synchronous = bool(o)
}

func WithSuppressErrors(v bool) Option {
	return suppressErrorsOption(v)
}

type suppressErrorsOption bool

func (o suppressErrorsOption) apply(h *Hook) {
	h.suppressErrors = bool(o)
}
