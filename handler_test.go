package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type TestType uint

const (
	// see TestLokiHook
	defaultTest TestType = iota

	// see TestFormat
	formatTest

	// see TestFlush
	flushTest

	// see TestNameAttribute
	nameAttrTest

	// see TestLabel
	labelTest

	// see TestLabelsEnabled
	labelEnabledTest

	// see TestHandler
	handlerTest

	// see TestMinLevel
	minLevelTest

	// see TestFlushWait
	flushWaitTest

	// see TestBatchInterval
	batchIntervalTest

	// see TestBatchSize
	batchSizeTest

	// see TestSynchronous
	synchronousTest

	// see TestErrors
	errorsTest

	// see TestIgnoreErrors
	errorsIgnoreTest
)

// TestLokiHandler tests:
// - different levels
// - labels at default settings
// - increasing dates at loki values
// - log messages
func TestLokiHandler(t *testing.T) {
	msgs, err := testInternal(defaultTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 0, "one label expected")
	require.Len(t, s.Values, 4, "4 log values expected")

	levels := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	var last time.Time
	for i, val := range s.Values {
		expected := fmt.Sprintf("level=%s msg=test", levels[i].String())

		require.True(t, !last.After(val.time), "logs should have a monotonously increasing timestamp")
		requireMessage(t, expected, val.message)

		last = val.time
	}
}

func requireMessage(t *testing.T, expected, message string) {
	require.Truef(t, strings.HasSuffix(message, expected), "sent log message %q needs to have the suffix %q", message, expected)
}

// TestFormat tests:
// - if the sent log entry string has the correct format
func TestFormat(t *testing.T) {
	msgs, err := testInternal(formatTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Values, 5, "5 Loki values expected")

	expected := []string{
		`level=INFO msg="test test"`,
		`level=INFO msg="test=test"`,
		`level=INFO msg="\"test\""`,
		`level=INFO msg=test test="\"test\""`,
		`level=ERROR msg=test error=test`,
	}

	for i, v := range s.Values {
		requireMessage(t, expected[i], v.message)
	}
}

// TestFlush tests:
// - flush function
// - different log messages
// - increasing date in different log messages
func TestFlush(t *testing.T) {
	msgs, err := testInternal(flushTest)
	require.NoError(t, err)

	checkMessages12_3(t, msgs)

	v1 := msgs[0].Streams[0].Values[1].time // 2. value in 1. message
	v2 := msgs[1].Streams[0].Values[0].time // 1. value in 2. message

	require.True(t, v1.Before(v2), "logs should have a monotonously increasing timestamp")
}

// checkMessages12_3 tests if one loki messages contains the logs with "1" and "2" and the other contains the message "3".
// Used at the following tests:
// - TestFlush
// - TestBatchInterval
// - TestBatchSize
func checkMessages12_3(t *testing.T, msgs []*lokiMessage) {
	require.Len(t, msgs, 2, "2 Loki messages expected")

	for i, m := range msgs {
		require.Lenf(t, m.Streams, 1, "one Loki stream in message %d expected", i+1)

		s := m.Streams[0]
		if i == 0 {
			require.Lenf(t, s.Values, 2, "2 Loki values in message %d expected", i+1)

			requireMessage(t, "level=INFO msg=1", s.Values[0].message)
			requireMessage(t, "level=INFO msg=2", s.Values[1].message)
		} else {
			require.Lenf(t, s.Values, 1, "one Loki value in message %d expected", i+1)
			requireMessage(t, "level=INFO msg=3", s.Values[0].message)
		}
	}
}

// TestNameAttribute tests:
// - name attribute
func TestNameAttribute(t *testing.T) {
	msgs, err := testInternal(nameAttrTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 1, "one label expected")
	require.Contains(t, s.Stream, "name", `label "name" expected`)
}

// TestLabel tests:
// - added label
func TestLabel(t *testing.T) {
	msgs, err := testInternal(labelTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 1, "one label expected")
	require.Contains(t, s.Stream, "test", `label "test" expected`)
}

// TestLabel tests:
// - all available attributes as labels
func TestLabelsEnabled(t *testing.T) {
	msgs, err := testInternal(labelEnabledTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]

	labels := s.Stream
	require.Len(t, labels, 6, `expected 6 labels: "name", extra field "test", "time", "level", "func", "msg"`)

	for k, v := range labels {
		switch k {
		case "name":
			require.Equal(t, v, "test")
		case "test":
			require.Equal(t, v, "value")
		case "time":
			date, err := time.Parse(time.RFC3339Nano, v)
			require.NoError(t, err)

			since := time.Since(date)
			require.Less(t, since, 1*time.Second, "log time should be less than 1 second ago")
		case "level":
			require.Equal(t, v, slog.LevelInfo.String())
		case "func":
			parts := strings.Split(v, ":")
			require.Len(t, parts, 3, "malformed func value")
			require.Truef(t, strings.HasSuffix(parts[0], "_test.go"), `file name "%s" should have suffix "_test.go"`)

			_, err := strconv.Atoi(parts[1])
			require.NoError(t, err)
			require.Equal(t, parts[2], "doLog()")
		case "msg":
			require.Equal(t, v, "test")
		}
	}

	require.Len(t, s.Values, 1, "only one log entry expected")

	regex, err := regexp.Compile(`^name=test level=INFO msg=test func=.+_test.go:\d+:doLog\(\) test=value$`)
	if err != nil {
		t.Fatal(err)
	}

	require.Regexp(t, regex, s.Values[0].message)
}

// TestHandler tests:
// - log message with a different handler
func TestHandler(t *testing.T) {
	msgs, err := testInternal(handlerTest)
	require.NoError(t, err)

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	v := m.Streams[0].Values
	require.Len(t, v, 1, "1 log value expected")

	var data map[string]string
	err = json.Unmarshal([]byte(v[0].message), &data)
	require.NoError(t, err)

	require.Len(t, data, 3)
	require.Contains(t, data, "level")
	require.Contains(t, data, "msg")
	require.Contains(t, data, "time")
}

// TestMinimumLevel tests:
// - logging with a minimum log level
func TestMinimumLevel(t *testing.T) {
	msgs, err := testInternal(minLevelTest)
	require.NoError(t, err)

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	v := m.Streams[0].Values
	require.Len(t, v, 1, "1 log value expected")

	require.Equal(t, "level=warning msg=test", v[0].message, "unexpected message")
}

// TestBatchInterval tests:
// - flush and wait
// - if the time resets after a send
func TestFlushWait(t *testing.T) {
	msgs, err := testInternal(flushWaitTest)
	require.NoError(t, err)

	require.Len(t, msgs, 3, "3 Loki messages expected")

	for i, m := range msgs {
		require.Lenf(t, m.Streams, 1, "one Loki stream in message %d expected", i+1)

		s := m.Streams[0]
		require.Lenf(t, s.Values, 1, "one Loki value in message %d expected", i+1)
		require.Equal(t, s.Values[0].message, fmt.Sprintf("level=INFO msg=%d", i+1))
	}
}

// TestBatchInterval tests:
// - testing the batch interval
func TestBatchInterval(t *testing.T) {
	msgs, err := testInternal(batchIntervalTest)
	require.NoError(t, err)

	checkMessages12_3(t, msgs)
}

// TestBatchSize tests:
// - batch size of 2
func TestBatchSize(t *testing.T) {
	msgs, err := testInternal(batchSizeTest)
	require.NoError(t, err)

	checkMessages12_3(t, msgs)
}

// TestSynchronous tests:
// - synchronous logging
func TestSynchronous(t *testing.T) {
	msgs, err := testInternal(synchronousTest)
	require.NoError(t, err)

	require.Len(t, msgs, 3, "3 Loki messages expected")

	for i, m := range msgs {
		require.Lenf(t, m.Streams, 1, "one Loki stream in message %d expected", i+1)

		s := m.Streams[0]
		require.Lenf(t, s.Values, 1, "one Loki value in message %d expected", i+1)
		require.Equal(t, s.Values[0].message, fmt.Sprintf("level=INFO msg=%d", i+1))
	}
}

var errOutput strings.Builder

// TestErrors tests:
// - logged errors (error at creating the loki message)
func TestErrors(t *testing.T) {
	errOutput.Reset()

	msgs, err := testInternal(errorsTest)
	require.NoError(t, err)
	require.Len(t, msgs, 0, "no Loki message expected")

	output := strings.TrimSpace(errOutput.String())
	require.Equal(t, `level=ERROR msg="Failed to create loki value from entry: failed to create entry"`, output)
}

type errorHandler struct{}

// Check if the type satisfies the Handler interface.
var _ slog.Handler = (*errorHandler)(nil)

// Enabled implements the [slog.Handler.Enabled] function.
func (h *errorHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle implements the [slog.Handler.Handle] function.
func (h *errorHandler) Handle(_ context.Context, _ slog.Record) error {
	return errors.New("logging error")
}

// WithAttrs implements the [slog.Handler.WithAttrs] function.
func (h *errorHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

// WithGroup implements the [slog.Handler.WithGroup] function.
func (h *errorHandler) WithGroup(_ string) slog.Handler {
	return h
}

// TestIgnoreErrors tests:
// - ignore errors (error at creating the loki message)
func TestIgnoreErrors(t *testing.T) {
	msgs, err := testInternal(errorsIgnoreTest)
	require.NoError(t, err)
	require.Len(t, msgs, 0, "no Loki message expected")
}

type lokiMessage struct {
	Streams []*lokiStreamV `json:"streams"`
}

type lokiStreamV struct {
	Stream map[string]string `json:"stream"`
	Values []*lokiValue      `json:"values"`
}

func testInternal(typ TestType) ([]*lokiMessage, error) {
	var (
		messages []*lokiMessage
		err      error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, rerr := readLoki(w, r)
		if rerr != nil {
			err = rerr
		} else {
			messages = append(messages, m)
		}
	}))

	defer server.Close()

	// never add the source for tests
	options := []Option{WithHandler(func(w io.Writer) slog.Handler {
		return slog.NewTextHandler(w, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: false,
		})
	})}

	// add the custom test options
	options = append(options, getOptions(typ)...)

	h := NewHandler(server.URL, options...)
	logger := slog.New(h)

	doLog(typ, logger, h)
	h.Close()

	if err != nil {
		return nil, err
	}

	return messages, nil
}

func getOptions(typ TestType) []Option {
	switch typ {
	case defaultTest, formatTest, flushTest:
		return nil
	case nameAttrTest:
		return []Option{WithName("test")}
	case labelTest:
		return []Option{WithLabel("test", "test")}
	case labelEnabledTest:
		all := []Label{LabelAttrs, LabelTime, LabelLevel, LabelCaller, LabelMessage}
		return []Option{WithName("test"), WithLabelsEnabled(all...)}
	case handlerTest:
		return []Option{WithHandler(func(w io.Writer) slog.Handler {
			return slog.NewJSONHandler(w, &slog.HandlerOptions{
				AddSource: true,
			})
		})}
	case minLevelTest:
		return []Option{WithHandler(func(w io.Writer) slog.Handler {
			return slog.NewTextHandler(w, &slog.HandlerOptions{
				Level: slog.LevelWarn,
			})
		})}
	case flushWaitTest, batchIntervalTest:
		return []Option{WithBatchInterval(1 * time.Second)}
	case batchSizeTest:
		return []Option{WithBatchSize(2)}
	case synchronousTest:
		return []Option{WithSynchronous(true)}
	case errorsTest:
		return []Option{
			WithHandler(func(_ io.Writer) slog.Handler {
				return &errorHandler{}
			}),
			WithErrorHandler(func(err error) {
				fmt.Fprintln(&errOutput, err.Error())
			}),
		}
	case errorsIgnoreTest:
		return []Option{
			WithHandler(func(_ io.Writer) slog.Handler {
				return &errorHandler{}
			}),
			// no error handler
		}
	default:
		return nil
	}
}

func doLog(typ TestType, log *slog.Logger, hook *Handler) {
	switch typ {
	case defaultTest:
		log.Debug("test")
		log.Info("test")
		log.Warn("test")
		log.Error("test")
	case formatTest:
		log.Info("test test")
		log.Info("test=test")
		log.Info(`"test"`)
		log.Info("test", slog.String("test", `"test"`))
		log.Error("test", slog.Any("error", errors.New("test")))
	case flushTest:
		log.Info("1")
		log.Info("2")
		hook.Flush()
		log.Info("3")
	case nameAttrTest, labelTest:
		log.Info("test")
	case labelEnabledTest:
		log.Info("test", slog.String("test", "value"))
	case handlerTest:
		log.Info("test")
	case minLevelTest:
		log.Debug("test")
		log.Info("test")
		log.Warn("test")
	case flushWaitTest:
		log.Info("1")
		hook.Flush()
		log.Info("2")
		time.Sleep(1100 * time.Millisecond) // 1.1 sec
		log.Info("3")
	case batchIntervalTest:
		log.Info("1")
		log.Info("2")
		time.Sleep(1100 * time.Millisecond) // 1.1 sec
		log.Info("3")
	case batchSizeTest, synchronousTest:
		log.Info("1")
		log.Info("2")
		log.Info("3")
	case errorsTest:
		log.Info("test")
	case errorsIgnoreTest:
		log.Info("test")
	default:
		break
	}
}

func readLoki(w http.ResponseWriter, r *http.Request) (*lokiMessage, error) {
	if r.Method != "POST" || r.URL.String() != "/loki/api/v1/push" {
		err := errors.New("unknown request")
		http.Error(w, err.Error(), 404)

		return nil, err
	}

	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return nil, err
	}

	var v lokiMessage
	err = json.Unmarshal(body, &v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return nil, err
	}

	w.WriteHeader(204)
	w.Write([]byte("OK"))
	return &v, nil
}

func (v *lokiValue) UnmarshalJSON(data []byte) error {
	r := bytes.NewReader(data)
	d := json.NewDecoder(r)

	done := false
	i := 0
	arrIdx := 0

	for ; d.More(); i++ {
		tk, err := d.Token()
		if err != nil {
			return err
		}

		if done {
			return fmt.Errorf("unexpected token `%v`", tk)
		}

		switch val := tk.(type) {
		case json.Delim:
			switch val {
			case '[':
				if i != 0 {
					return errors.New("unexpected array")
				}
			case ']':
				done = true
			default:
				return fmt.Errorf("unexpected delimiter '%c'", val)
			}
		case string:
			switch arrIdx {
			case 0:
				ns, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return fmt.Errorf("parsing date: %v", err)
				}

				v.time = time.Unix(0, ns)
			case 1:
				v.message = val
			default:
				return fmt.Errorf("unexpected value at array index %d", arrIdx)
			}

			arrIdx++
		}
	}

	return nil
}
