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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type TestType uint

const (
	// see TestLokiHandler
	defaultTest TestType = iota

	// see TestAttrAndGroup
	attrAndGroupTest

	// see TestAttrAndGroupLabels
	attrAndGroupLabelsTest

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

// TestAttrAndGroup tests:
// - if attributes and groups are correctly added to log messages
func TestAttrAndGroup(t *testing.T) {
	msgs, err := testInternal(attrAndGroupTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Empty(t, s.Stream, "no Loki stream labels expected")

	expected := []string{
		"level=INFO msg=test",
		"level=INFO msg=test test1=value1 test2=value2 group.test1=value1 group.test2=value2 group.inner.test=value",
		"level=INFO msg=test test1=value1 test2=value2",
		"level=INFO msg=test group.test=value",
		"level=INFO msg=test group.test1=value1 group.test2=value2",
		"level=INFO msg=test group.test1=value1 group.inner.test2=value2",
	}

	require.Len(t, s.Values, len(expected), "6 Loki values expected")

	for i, msg := range expected {
		requireMessage(t, msg, s.Values[i].message)
	}
}

// TestAttrAndGroupLabels tests:
// - if attributes and groups are correctly added to log messages and as labels
func TestAttrAndGroupLabels(t *testing.T) {
	msgs, err := testInternal(attrAndGroupLabelsTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki messages expected")

	m := msgs[0]
	require.Len(t, m.Streams, 6, "6 Loki streams expected")

	expectedLabels := []map[string]string{
		{},
		{
			"test1":            "value1",
			"test2":            "value2",
			"group.test1":      "value1",
			"group.test2":      "value2",
			"group.inner.test": "value",
		},
		{
			"test1": "value1",
			"test2": "value2",
		},
		{
			"group.test": "value",
		},
		{
			"group.test1": "value1",
			"group.test2": "value2",
		},
		{
			"group.test1":       "value1",
			"group.inner.test2": "value2",
		},
	}

	expectedMsgs := []string{
		"level=INFO msg=test",
		"level=INFO msg=test test1=value1 test2=value2 group.test1=value1 group.test2=value2 group.inner.test=value",
		"level=INFO msg=test test1=value1 test2=value2",
		"level=INFO msg=test group.test=value",
		"level=INFO msg=test group.test1=value1 group.test2=value2",
		"level=INFO msg=test group.test1=value1 group.inner.test2=value2",
	}

	for i, labels := range expectedLabels {
		s := m.Streams[i]

		require.Equal(t, labels, s.Stream, "labels does not match")
		require.Len(t, s.Values, 1, "expected one Loki value")
		requireMessage(t, expectedMsgs[i], s.Values[0].message)
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
	require.Len(t, m.Streams, 3, "3 Loki stream expected")

	for i, s := range m.Streams {
		labels := s.Stream

		switch i {
		case 0:
			requireKeys(t, []string{"name", "time", "level", "func", "msg"}, labels)
		case 1:
			requireKeys(t, []string{"name", "time", "level", "func", "msg", "test"}, labels)
		case 2:
			requireKeys(t, []string{"name", "time", "level", "func", "msg", "test", "group.test1", "group.test2"}, labels)
		}

		for k, v := range labels {
			switch k {
			case "name":
				require.Equal(t, v, "test")
			case "test":
				if i != 0 {
					require.Equal(t, v, "value")
				}
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
			case "group.test1":
				if i == 2 {
					require.Equal(t, v, "value1")
				}
			case "group.test2":
				if i == 2 {
					require.Equal(t, v, "value2")
				}
			}
		}

		require.Len(t, s.Values, 1, "only one log message expected")

		msg := s.Values[0].message

		switch i {
		case 0:
			requireMessage(t, "level=INFO msg=test", msg)
		case 1:
			requireMessage(t, "level=INFO msg=test test=value", msg)
		case 2:
			requireMessage(t, "level=INFO msg=test test=value group.test1=value1 group.test2=value2", msg)
		}
	}
}

func requireKeys(t *testing.T, expected []string, m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	require.ElementsMatch(t, expected, keys)
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

	var data map[string]any
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

	requireMessage(t, "level=WARN msg=test", v[0].message)
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
		requireMessage(t, fmt.Sprintf("level=INFO msg=%d", i+1), s.Values[0].message)
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
		requireMessage(t, fmt.Sprintf("level=INFO msg=%d", i+1), s.Values[0].message)
	}
}

type lokiMessage struct {
	Streams []*lokiStream `json:"streams"`
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
		return NewLogfmtHandler(w, &LogfmtOptions{
			Level: slog.LevelDebug,
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
	case defaultTest:
		return nil
	case attrAndGroupTest:
		return nil
	case attrAndGroupLabelsTest:
		return []Option{WithLabelsEnabled(LabelAttrs)}
	case formatTest:
		return nil
	case flushTest:
		return nil
	case nameAttrTest:
		return []Option{WithName("test")}
	case labelTest:
		return []Option{WithLabel("test", "test")}
	case labelEnabledTest:
		all := []Label{LabelAttrs, LabelTime, LabelLevel, LabelSource, LabelMessage}
		return []Option{WithName("test"), WithLabelsEnabled(all...)}
	case handlerTest:
		return []Option{WithHandler(func(w io.Writer) slog.Handler {
			return slog.NewJSONHandler(w, nil)
		})}
	case minLevelTest:
		return []Option{WithHandler(func(w io.Writer) slog.Handler {
			return NewLogfmtHandler(w, &LogfmtOptions{
				Level: slog.LevelWarn,
			})
		})}
	case flushWaitTest, batchIntervalTest:
		return []Option{WithBatchInterval(1 * time.Second)}
	case batchSizeTest:
		return []Option{WithBatchSize(2)}
	case synchronousTest:
		return []Option{WithSynchronous(true)}
	default:
		return nil
	}
}

func doLog(typ TestType, log *slog.Logger, h *Handler) {
	switch typ {
	case defaultTest:
		log.Debug("test")
		log.Info("test")
		log.Warn("test")
		log.Error("test")
	case attrAndGroupTest, attrAndGroupLabelsTest:
		// regular log message
		log.Info("test")

		// with attributes and group
		log.Info("test",
			slog.String("test1", "value1"),
			slog.String("test2", "value2"),
			slog.Group("group",
				slog.String("test1", "value1"),
				slog.String("test2", "value2"),
				slog.Group("inner",
					slog.String("test", "value"),
				),
			),
		)

		// with logger having attributes
		l := log.With(slog.String("test1", "value1"))
		l.Info("test", slog.String("test2", "value2"))

		// with logger having a group
		l = log.WithGroup("group")
		l.Info("test", slog.String("test", "value"))

		// with logger having attributes and group
		l = log.WithGroup("group").With(slog.String("test1", "value1"))
		l.Info("test", slog.String("test2", "value2"))

		// ... with inner group
		l = l.WithGroup("inner")
		l.Info("test", slog.String("test2", "value2"))
	case formatTest:
		log.Info("test test")
		log.Info("test=test")
		log.Info(`"test"`)
		log.Info("test", slog.String("test", `"test"`))
		log.Error("test", slog.Any("error", errors.New("test")))
	case flushTest:
		log.Info("1")
		log.Info("2")
		h.Flush()
		log.Info("3")
	case nameAttrTest, labelTest:
		log.Info("test")
	case labelEnabledTest:
		log.Info("test")
		log.Info("test", slog.String("test", "value"))
		log.Info("test",
			slog.String("test", "value"),
			slog.Group("group",
				slog.String("test1", "value1"),
				slog.String("test2", "value2"),
			),
		)
	case handlerTest:
		log.Info("test")
	case minLevelTest:
		log.Debug("test")
		log.Info("test")
		log.Warn("test")
	case flushWaitTest:
		log.Info("1")
		h.Flush()
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

	var bb bytes.Buffer
	json.Indent(&bb, body, "", "  ")
	// fmt.Println(bb.String())

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
