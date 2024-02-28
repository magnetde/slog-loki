package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	labels        map[string]any
	labelsEnabled []Label
	labelAttrs    bool // if labelsEnabled contains LabelAttr; for performance

	// log handling
	buflock sync.Mutex
	strbuf  strings.Builder
	handler slog.Handler

	// settings for sync / async behavior
	synchronous   bool
	batchInterval time.Duration
	batchSize     int
	errHandler    func(err error)

	// mechanics for async mode
	mu      sync.Mutex
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

// lokiPair stores a map of Loki labels and a loki value.
// It is used for sending Loki log messages to the background worker.
type lokiRecord struct {
	value  *lokiValue
	labels map[string]string
}

type subhandler struct {
	root    *Handler
	handler slog.Handler
	prefix  string
	labels  map[string]string
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
		labels:        make(map[string]any),
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
		h.handler = slog.NewTextHandler(&h.strbuf, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})
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
	return h.handle(h.handler, nil, ctx, r)
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var l map[string]string
	if h.labelAttrs && len(attrs) > 0 {
		l = make(map[string]string, len(attrs))

		for _, a := range attrs {
			addAttr(l, "", a)
		}
	}

	return &subhandler{
		root:    h,
		handler: h.handler.WithAttrs(attrs),
		prefix:  "",
		labels:  l,
	}
}

func addAttr(labels map[string]string, prefix string, a slog.Attr) {
	k := a.Key
	v := a.Value

	switch v.Kind() {
	case slog.KindGroup:
		prefix := k + "."

		for _, a := range v.Group() {
			addAttr(labels, prefix, a)
		}
	case slog.KindLogValuer:
		v = v.LogValuer().LogValue()
		fallthrough
	default:
		labels[prefix+k] = v.String()
	}
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &subhandler{
		root:    h,
		handler: h.handler.WithGroup(name),
		prefix:  name + ".",
		labels:  nil,
	}
}

// Handle implements the [slog.Handler.Handle] function.
func (h *subhandler) Handle(ctx context.Context, r slog.Record) error {
	return h.root.handle(h.handler, h.labels, ctx, r)
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *subhandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	l := maps.Clone(h.labels)
	if h.root.labelAttrs && len(attrs) > 0 {
		if l == nil {
			l = make(map[string]string, len(attrs))
		}

		for _, a := range attrs {
			addAttr(l, "", a)
		}
	}

	return &subhandler{
		root:    h.root,
		handler: h.handler.WithAttrs(attrs),
		prefix:  h.prefix,
		labels:  l,
	}
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *subhandler) WithGroup(name string) slog.Handler {
	return &subhandler{
		root:    h.root,
		handler: h.handler.WithGroup(name),
		prefix:  h.prefix + name + ".",
		labels:  maps.Clone(h.labels),
	}
}

func (h *Handler) handle(handler slog.Handler, labels map[string]string, ctx context.Context, r slog.Record) error {
	v, err := h.lokiValue(handler, ctx, r)
	if err != nil {
		return err
	}

	l := h.lokiLabels(labels, &r)

	if h.synchronous {
		s := []*lokiStream{{
			Stream: l,
			Values: []*lokiValue{v},
		}}
		return h.send(s)
	}

	h.wgBuf.Add(1)
	h.buf <- lokiRecord{
		value:  v,
		labels: l,
	}

	return nil
}

// Flush waits for the log queue to be empty.
// This func is meant to be used when the hook was created as asynchronous.
func (h *Handler) Flush() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.synchronous {
		h.doFlush()
	}
}

// Close waits for the log queue to be empty and then stops the background worker.
// This func is meant to be used when the hook was created as asynchronous.
func (h *Handler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.synchronous {
		h.doFlush()
		close(h.flush)
		close(h.buf)
	}
}

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
			h.wgBuf.Done()

			sent := h.bufEntry(p.value, p.labels)

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

func (h *Handler) bufEntry(v *lokiValue, l map[string]string) bool {
	sent := false

	// check if there is already a stream with matching labels
	i := slices.IndexFunc(h.bufStream, func(s *lokiStream) bool {
		return maps.Equal(s.Stream, l)
	})

	if i >= 0 { // stream found
		s := h.bufStream[i]

		s.Values = append(s.Values, v)
	} else {
		s := &lokiStream{
			Stream: l,
			Values: []*lokiValue{v},
		}

		h.bufStream = append(h.bufStream, s)
	}

	h.bufStreamSize++

	if h.bufStreamSize >= h.batchSize {
		sent = h.sendBuffer()
	}

	return sent
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

	errstr := fmt.Sprintf("unexpected HTTP status code %d", res.StatusCode)

	if res.ContentLength > 0 {
		errstr += ", response: "

		if res.ContentLength < 1024 {
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}

			errstr += string(body)
		} else {
			errstr += fmt.Sprintf("%d bytes", res.ContentLength)
		}
	}

	return errors.New(errstr)
}

func marshalStream(buf *bytes.Buffer, stream []*lokiStream) error {
	buf.WriteString(`{"streams":`)

	data, err := json.Marshal(stream)
	if err != nil {
		return err
	}

	buf.Write(data)
	buf.WriteByte('}')

	return nil
}

// marshalString marshals the string into the buffer.
func marshalString(buf *bytes.Buffer, s string) {
	if needsMarshalling(s) {
		key, _ := json.Marshal(s)
		buf.Write(key)
	} else {
		buf.WriteByte('"')
		buf.WriteString(s)
		buf.WriteByte('"')
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

// Enabled implements the [slog.Handler.Enabled] function.
func (h *subhandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.handler.Enabled(ctx, lvl)
}
