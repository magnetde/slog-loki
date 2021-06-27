package loki

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Option is the parameter type for options when initializing the log hook.
type Option interface {
	apply(h *Hook)
}

func WithTypeAttribute(v string) Option {
	return typeAttrOption(v)
}

type typeAttrOption string

func (o typeAttrOption) apply(h *Hook) {
	h.typeAttr = string(o)
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
