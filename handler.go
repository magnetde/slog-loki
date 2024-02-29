package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// BufSize is used as the channel size which buffers log entries before sending them asynchronously to the loki server.
// Set loki.BufSize = <value> _before_ calling NewHandler
// Once the buffer is full, logging will start blocking, waiting for slots to be available in the queue.
var BufSize uint = 4096

// Handler is the slog handler for Loki.
type Handler struct {
	url string

	// label settings
	labels        map[string]string
	labelsEnabled []Label
	labelAttrs    bool // if labelsEnabled contains LabelAttr; for performance

	// log handling
	buflock        sync.Mutex
	strbuf         strings.Builder
	handler        slog.Handler
	defaultHandler bool

	// settings for sync / async behavior
	synchronous   bool
	batchInterval time.Duration
	batchSize     int
	errHandler    func(err error)

	// mechanics for async mode
	mu      sync.Mutex
	closed  bool
	flush   chan struct{}
	buf     chan lokiRecord
	wgBuf   sync.WaitGroup
	wgFlush sync.WaitGroup

	// Buffer values for async logging.
	// They are only modified at the "sendBuffer" function, which called from the worker goroutine,
	// so no mutex mechanism is necessary.
	bufStream     []*lokiStream
	bufStreamSize int
}

// lokiRecord stores a map of Loki labels and a loki value.
// It is used for sending Loki log messages to the background worker.
type lokiRecord struct {
	value  *lokiValue
	labels map[string]string
}

// subhandler is used as a slog handler for loggers created with "WithAttrs" and "WithGroup".
type subhandler struct {
	root    *Handler
	handler slog.Handler
	prefix  string
	attrs   map[string]string
}

// Test if the type satisfies the slog.Handler interface.
var (
	_ slog.Handler = (*Handler)(nil)
	_ slog.Handler = (*subhandler)(nil)
)

// NewHandler creates a slog handler for Loki.
// Parameters:
//
//	url: base url of Loki
//	options: Options for this handler; see README.md
func NewHandler(url string, options ...Option) *Handler {
	if url == "" {
		url = "http://localhost:3100"
	}

	h := &Handler{
		url: url,

		// default values
		labels:        nil,
		labelsEnabled: nil,
		labelAttrs:    false,
		batchInterval: 15 * time.Second,
		batchSize:     1024,
		synchronous:   false,
		errHandler:    nil,
	}

	for _, o := range options {
		o.apply(h)
	}

	if h.handler == nil {
		h.handler = NewLogfmtHandler(&h.strbuf, nil)
		h.defaultHandler = true
	}

	if !h.synchronous {
		h.flush = make(chan struct{})
		h.buf = make(chan lokiRecord, BufSize)
		h.bufStream = nil

		go h.worker()
	}

	return h
}

// Enabled implements the [slog.Handler.Enabled] function.
func (h *Handler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.handler.Enabled(ctx, lvl)
}

// Handle implements the [slog.Handler.Handle] function.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	v, err := h.lokiValue(ctx, h.handler, r)
	if err != nil {
		return err
	}

	l := h.lokiLabels("", nil, &r)

	return h.handle(v, l)
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var l map[string]string
	if h.labelAttrs && len(attrs) > 0 {
		l = make(map[string]string, len(attrs))
		addAttrs(l, "", attrs...)
	}

	return &subhandler{
		root:    h,
		handler: h.handler.WithAttrs(attrs),
		prefix:  "",
		attrs:   l,
	}
}

// addAttrs adds the attributes `a` to map `labels` with prefix `prefix`.
// All attributes are unpacked and converted to key value pairs of type string.
func addAttrs(labels map[string]string, prefix string, a ...slog.Attr) {
	for _, a := range a {
		unpackAttr(prefix, a, func(k, v string) {
			labels[k] = v
		})
	}
}

// unpackAttr unpacks an attribute by converting it into a key value pair of type string.
// Groups are unpacked into a list of multiple attributes.
// The unpacked attributes are returned via the delivery function.
func unpackAttr(prefix string, a slog.Attr, deliver func(k, v string)) {
	k := a.Key
	v := a.Value

	if v.Kind() == slog.KindGroup {
		prefix := prefix + k + "."

		for _, a := range v.Group() {
			unpackAttr(prefix, a, deliver)
		}
	} else {
		v = v.Resolve()
		deliver(prefix+k, v.String())
	}
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &subhandler{
		root:    h,
		handler: h.handler.WithGroup(name),
		prefix:  name + ".",
		attrs:   nil,
	}
}

// Enabled implements the [slog.Handler.Enabled] function.
func (h *subhandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.handler.Enabled(ctx, lvl)
}

// Handle implements the [slog.Handler.Handle] function.
func (h *subhandler) Handle(ctx context.Context, r slog.Record) error {
	v, err := h.root.lokiValue(ctx, h.handler, r)
	if err != nil {
		return err
	}

	l := h.root.lokiLabels(h.prefix, h.attrs, &r)

	return h.root.handle(v, l)
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *subhandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	l := maps.Clone(h.attrs)
	if h.root.labelAttrs && len(attrs) > 0 {
		if l == nil {
			l = make(map[string]string, len(attrs))
		}

		addAttrs(l, h.prefix, attrs...)
	}

	return &subhandler{
		root:    h.root,
		handler: h.handler.WithAttrs(attrs),
		prefix:  h.prefix,
		attrs:   l,
	}
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *subhandler) WithGroup(name string) slog.Handler {
	return &subhandler{
		root:    h.root,
		handler: h.handler.WithGroup(name),
		prefix:  h.prefix + name + ".",
		attrs:   h.attrs, // reuse because they will not get modified
	}
}

// handle is the internal handler function.
// It handles a Loki value with a map of labels.
func (h *Handler) handle(value *lokiValue, labels map[string]string) error {
	if h.synchronous {
		s := []*lokiStream{{
			Stream: labels,
			Values: []*lokiValue{value},
		}}

		return h.send(s)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return errors.New("handler closed")
	}

	h.wgBuf.Add(1)
	h.buf <- lokiRecord{
		value:  value,
		labels: labels,
	}

	return nil
}

// Flush waits for the log queue to be empty.
// This function is meant to be used when the handler was created as asynchronous.
func (h *Handler) Flush() {
	if h.synchronous {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}

	h.doFlush()
}

// Close waits for the log queue to be empty and then stops the background worker.
// This func is meant to be used when the handler was created as asynchronous.
func (h *Handler) Close() {
	if h.synchronous {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.doFlush()
	close(h.flush)
	close(h.buf)
	h.closed = true
}

// doFlush flushes the buffer.
// Should only be called when the handler is locked.
func (h *Handler) doFlush() {
	h.wgBuf.Wait()

	h.wgFlush.Add(1)
	h.flush <- struct{}{}
	h.wgFlush.Wait()
}

// process runs the worker queue in the background
func (h *Handler) worker() {
	wait := time.NewTimer(h.batchInterval)

loop:
	for {
		select {
		case p := <-h.buf:
			sent := h.bufValue(p.value, p.labels)
			h.wgBuf.Done()

			if sent {
				wait.Reset(h.batchInterval)
			}
		case <-wait.C:
			h.sendBuffer()

			wait.Reset(h.batchInterval)
		case _, ok := <-h.flush:
			if ok {
				h.sendBuffer()
				h.wgFlush.Done()

				wait.Reset(h.batchInterval)
			} else {
				break loop
			}
		}
	}

	if !wait.Stop() {
		<-wait.C
	}
}

// bufValue adds a new loki value to the buffer.
func (h *Handler) bufValue(value *lokiValue, labels map[string]string) bool {
	// check if there is already a stream with matching labels
	i := slices.IndexFunc(h.bufStream, func(s *lokiStream) bool {
		return maps.Equal(s.Stream, labels)
	})

	if i >= 0 { // stream found; append to the found stream
		s := h.bufStream[i]

		s.Values = append(s.Values, value)
	} else { // no stream found; add a new stream
		s := &lokiStream{
			Stream: labels,
			Values: []*lokiValue{value},
		}

		h.bufStream = append(h.bufStream, s)
	}

	h.bufStreamSize++

	if h.bufStreamSize >= h.batchSize {
		return h.sendBuffer()
	}

	return false
}

// sendBuffer sends the Loki message and then logs errors to the console.
func (h *Handler) sendBuffer() bool {
	if len(h.bufStream) == 0 {
		return false
	}

	err := h.send(h.bufStream)
	if err != nil && h.errHandler != nil {
		h.errHandler(err)
	}

	h.bufStream = h.bufStream[:0]
	h.bufStreamSize = 0

	return true
}

// send sends the Loki message.
func (h *Handler) send(stream []*lokiStream) error {
	var b bytes.Buffer
	marshalStream(&b, stream)

	res, err := http.Post(h.url+"/loki/api/v1/push", "application/json", &b)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode == 204 {
		return nil
	}

	return fmt.Errorf("unexpected HTTP status code %d", res.StatusCode)
}

// marshalStream writes the JSON encoding of the current stream.
func marshalStream(b *bytes.Buffer, stream []*lokiStream) error {
	data, err := json.Marshal(stream)
	if err != nil {
		return err
	}

	b.Grow(len(data) + 12)
	b.WriteString(`{"streams":`)
	b.Write(data)
	b.WriteByte('}')

	return nil
}

// marshalString marshals the string into the buffer.
func marshalString(b *bytes.Buffer, s string) {
	if needsMarshalling(s) {
		key, _ := json.Marshal(s)
		b.Write(key)
	} else {
		b.WriteByte('"')
		b.WriteString(s)
		b.WriteByte('"')
	}
}

// needsMarshalling checks, if for a string a call to json.Marshal is needed.
func needsMarshalling(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > 0x7f || s[i] == '"' || s[i] == '\\' {
			return true
		}
	}
	return false
}
