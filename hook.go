package loki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// BufSize is used as the channel size which buffers log entries before sending them asynchrously to the loki server.
// Set loki.BufSize = <value> _before_ calling NewServerHook
// Once the buffer is full, logging will start blocking, waiting for slots to be available in the queue.
var BufSize uint = 4096

// ServerHook to send logs to logcollect server.
type Hook struct {
	src string
	url string

	srcAttr string
	labels  []Label

	formatter    logrus.Formatter
	removeColors bool
	minLevel     logrus.Level

	batchInterval time.Duration
	batchSize     int

	synchronous    bool
	suppressErrors bool

	quit chan struct{}
	buf  chan *logrus.Entry
	wg   sync.WaitGroup
	mu   sync.RWMutex
}

// Test if the ServerHook matches the logrus.Hook interface.
var _ logrus.Hook = (*Hook)(nil)

// NewHook creates a hook to be added to an instance of logger.
func NewHook(src, url string, options ...Option) (*Hook, error) {
	if src == "" {
		return nil, errors.New("empty log type")
	}
	if url == "" {
		return nil, errors.New("empty url")
	}

	h := &Hook{
		src: src,
		url: url,

		// default values
		srcAttr:        "source",
		labels:         []Label{SourceLabel},
		formatter:      &logrus.TextFormatter{DisableTimestamp: true},
		removeColors:   false,
		minLevel:       logrus.TraceLevel,
		batchInterval:  10 * time.Second,
		batchSize:      1000,
		synchronous:    false,
		suppressErrors: false,
	}

	for _, o := range options {
		o.apply(h)
	}

	if !h.synchronous {
		h.buf = make(chan *logrus.Entry, BufSize)

		go h.worker()
	}

	return h, nil
}

// Fire sends a log entry to the server.
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

	h.wg.Add(1)
	h.buf <- newEntry

	if entry.Level == logrus.PanicLevel || entry.Level == logrus.FatalLevel {
		h.wg.Wait()
	}

	return nil
}

// Flush waits for the log queue to be empty.
// This func is meant to be used when the hook was created as asynchronous.
func (h *Hook) Flush() {
	h.mu.Lock() // claim the mutex as a Lock - we want exclusive access to it
	defer h.mu.Unlock()

	if !h.synchronous {
		close(h.quit)
		h.wg.Wait()
	}

}

// Levels returns the Levels used for this hook.
func (h *Hook) Levels() []logrus.Level {
	levels := make([]logrus.Level, 0, int(h.minLevel)+1) // capacity: minlvl+1

	for _, l := range logrus.AllLevels {
		if l <= h.minLevel {
			levels = append(levels, l)
		}
	}

	return levels
}

// process runs the worker queue in the background
func (h *Hook) worker() {
	maxWait := time.NewTimer(h.batchInterval)

	var (
		labels lokiLabels
		values []*lokiValue
	)

	for {
		select {
		case <-h.quit:
			if len(values) > 0 {
				h.sendLogError(labels, values)
			}

			h.wg.Done()
			return
		case e := <-h.buf:
			l := h.lokiLabels(e)

			if !labels.equals(l) {
				if len(values) > 0 {
					h.sendLogError(labels, values)
					values = values[:0]
				}

				labels = l
			}

			val, err := h.lokiValue(e)
			if err != nil {
				logrus.Error("Failed to create loki value from entry: " + err.Error())
				break
			}

			values = append(values, val)

			if len(values) >= h.batchSize {
				h.sendLogError(labels, values)
				values = values[:0]

				maxWait.Reset(h.batchInterval)
			}
		case <-maxWait.C:
			if len(values) > 0 {
				h.sendLogError(labels, values)
				values = values[:0]
			}

			maxWait.Reset(h.batchInterval)
		}
	}
}

func (h *Hook) sendLogError(l lokiLabels, values []*lokiValue) {
	err := h.send(l, values)
	if err != nil && !h.suppressErrors {
		logrus.Error("Failed to send entry to loki: " + err.Error())
	}
}

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

	client := http.Client{}

	res, err := client.Post(h.url, "application/json", bytes.NewBuffer(jsonMsg))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == 204 {
		return nil
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	return fmt.Errorf("unexpected HTTP status code: %d, message: %s", res.StatusCode, body)
}
