package loki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// BufSize is used as the channel size which buffers log entries before sending them asynchrously to the loki server.
// Set loki.BufSize = <value> _before_ calling NewServerHook
// Once the buffer is full, logging will start blocking, waiting for slots to be available in the queue.
var BufSize uint = 4096

// Hook is the hook that can be used to log to Loki.
type Hook struct {
	url string

	// settings
	labels        map[string]interface{}
	labelsEnabled []Label

	formatter logrus.Formatter
	level     logrus.Level

	batchInterval time.Duration
	batchSize     int

	synchronous    bool
	suppressErrors bool

	// mechanics for async mode
	flush   chan struct{}
	buf     chan *logrus.Entry
	wgBuf   sync.WaitGroup
	wgFlush sync.WaitGroup
	mu      sync.RWMutex

	// buffer values for async
	bufLabels lokiLabels
	bufValues []*lokiValue
}

// Test if the ServerHook matches the logrus.Hook interface.
var _ logrus.Hook = (*Hook)(nil)

// NewHook creates a hook to be added to an instance of logger.
// Parameters:
//  url: base url of Loki
//  options: Ooptions for this hook; see README.md
func NewHook(url string, options ...Option) *Hook {
	if url == "" {
		url = "http://localhost:3100"
	}

	h := &Hook{
		url: url,

		// default values
		labels:         make(map[string]interface{}),
		labelsEnabled:  nil,
		formatter:      &logfmtFormatter{removeColors: false},
		level:          logrus.TraceLevel,
		batchInterval:  10 * time.Second,
		batchSize:      1000,
		synchronous:    false,
		suppressErrors: false,
	}

	for _, o := range options {
		o.apply(h)
	}

	if !h.synchronous {
		h.flush = make(chan struct{})
		h.buf = make(chan *logrus.Entry, BufSize)
		h.bufLabels = make(lokiLabels)
		h.bufValues = make([]*lokiValue, 0)

		go h.worker()
	}

	return h
}

// Fire sends a log entry to Loki.
// To prevent data races in asynchronous mode, a new entry is created and then sent to the channel.
func (h *Hook) Fire(entry *logrus.Entry) error {
	h.mu.RLock() // Claim the mutex as a RLock - allowing multiple go routines to log simultaneously
	defer h.mu.RUnlock()

	if h.synchronous {
		l := h.lokiLabels(entry)

		v, err := h.lokiValue(entry)
		if err != nil {
			return err
		}

		vs := []*lokiValue{v}
		return h.send(l, vs)
	}

	// Creating a new entry to prevent data races
	newData := make(map[string]interface{})
	for k, v := range entry.Data {
		newData[k] = v
	}

	newEntry := &logrus.Entry{
		Logger:  entry.Logger,
		Data:    newData,
		Time:    entry.Time,
		Level:   entry.Level,
		Caller:  entry.Caller,
		Message: entry.Message,
	}

	h.wgBuf.Add(1)
	h.buf <- newEntry

	if entry.Level == logrus.PanicLevel || entry.Level == logrus.FatalLevel {
		h.doFlush()
	}

	return nil
}

func (h *Hook) doFlush() {
	h.wgBuf.Wait()

	h.wgFlush.Add(1)
	h.flush <- struct{}{}
	h.wgFlush.Wait()
}

// Flush waits for the log queue to be empty.
// This func is meant to be used when the hook was created as asynchronous.
func (h *Hook) Flush() {
	h.mu.Lock() // claim the mutex as a Lock - we want exclusive access to it
	defer h.mu.Unlock()

	if !h.synchronous {
		h.doFlush()
	}
}

// Close waits for the log queue to be empty and then stops the background worker.
// This func is meant to be used when the hook was created as asynchronous.
func (h *Hook) Close() {
	h.mu.Lock() // claim the mutex as a Lock - we want exclusive access to it
	defer h.mu.Unlock()

	if !h.synchronous {
		h.doFlush()
		close(h.flush)
		close(h.buf)
	}
}

// Levels returns the Levels used for this hook.
func (h *Hook) Levels() []logrus.Level {
	levels := make([]logrus.Level, 0, int(h.level)+1) // capacity: minlvl+1

	for _, l := range logrus.AllLevels {
		if l <= h.level {
			levels = append(levels, l)
		}
	}

	return levels
}

// process runs the worker queue in the background
func (h *Hook) worker() {
	maxWait := time.NewTimer(h.batchInterval)

loop:
	for {
		select {
		case _, ok := <-h.flush:
			if ok {
				h.sendBuffer()
				h.wgFlush.Done()

				maxWait.Reset(h.batchInterval)
			} else {
				break loop
			}
		case e := <-h.buf:
			h.wgBuf.Done()

			sent, err := h.bufEntry(e)
			if err != nil {
				h.logError("Failed to create loki value from entry: " + err.Error())
				break
			}

			if sent {
				maxWait.Reset(h.batchInterval)
			}
		case <-maxWait.C:
			h.sendBuffer()

			maxWait.Reset(h.batchInterval)
		}
	}

	if !maxWait.Stop() {
		<-maxWait.C
	}
}

func (h *Hook) bufEntry(e *logrus.Entry) (bool, error) {
	sent := false

	l := h.lokiLabels(e)

	if !h.bufLabels.equals(l) { // label differ; send buffer
		sent = h.sendBuffer()
		h.bufLabels = l
	}

	val, err := h.lokiValue(e)
	if err != nil {
		return false, err
	}

	h.bufValues = append(h.bufValues, val)

	if len(h.bufValues) >= h.batchSize {
		sent = h.sendBuffer()
	}

	return sent, nil
}

// sendLogError sends the Loki message and then logs errors to the console.
func (h *Hook) sendBuffer() bool {
	if len(h.bufValues) == 0 {
		return false
	}

	err := h.send(h.bufLabels, h.bufValues)
	if err != nil {
		h.logError("Failed to send entry to loki: " + err.Error())
	}

	h.bufValues = h.bufValues[:0]
	return true
}

// send sends the Loki message by calling sendMessage().
func (h *Hook) send(l lokiLabels, values []*lokiValue) error {
	stream := &lokiStream{
		Stream: l,
		Values: values,
	}

	streams := []*lokiStream{stream}
	m := &lokiMessage{Streams: streams}

	return h.sendMessage(m)
}

func (h *Hook) sendMessage(m *lokiMessage) error {
	jsonMsg, err := json.Marshal(m)
	if err != nil {
		return err
	}

	res, err := http.Post(h.url+"/loki/api/v1/push", "application/json", bytes.NewBuffer(jsonMsg))
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

func (h *Hook) logError(str string) {
	if h.suppressErrors {
		return
	}

	f := logrus.StandardLogger().Formatter
	e := logrus.NewEntry(logrus.StandardLogger())

	e.Time = time.Now()
	e.Level = logrus.ErrorLevel
	e.Message = str

	if b, err := f.Format(e); err == nil {
		logrus.StandardLogger().Out.Write(b)
	}
}
