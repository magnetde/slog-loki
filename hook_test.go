package loki

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type TestType int

const (
	// see TestLokiHook
	defaultTest TestType = iota

	// see TestFlush
	flushTest

	// see TestSourceAttribute
	sourceAttrTest

	// see TestLabel
	labelTest

	// see TestLabelsEnabled
	labelEnabledTest

	// see TestFormatter
	formatterTest

	// see TestRemoveColors
	removeColorTest

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

	// see TestSuppressErrors
	suppressErrorsTest
)

// Initialize logrus and run all tests.
func TestMain(m *testing.M) {
	f := &logrus.TextFormatter{DisableTimestamp: true, DisableColors: true, CallerPrettyfier: callerPrettyfier}
	logrus.SetFormatter(f)

	code := m.Run()
	os.Exit(code)
}

// TestLokiHook tests:
// - different levels
// - labels at default settings
// - increasing dates at loki values
// - log messages
func TestLokiHook(t *testing.T) {
	msgs, err := testInternal(defaultTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 0, "one label expected")

	v := s.Values
	require.Len(t, v, 5, "5 log values expected")

	var last time.Time
	for i, val := range v {
		expectlv := logrus.AllLevels[len(logrus.AllLevels)-1-i]
		expected := fmt.Sprintf("level=%s msg=test", expectlv.String())

		require.True(t, !last.After(val.Date), "logs should have a monotonously increasing timestamp")
		require.Equal(t, expected, val.Message, "sent log message differ")

		last = val.Date
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

	v1 := msgs[0].Streams[0].Values[1].Date // 2. value in 1. message
	v2 := msgs[1].Streams[0].Values[0].Date // 1. value in 2. message

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

			require.Equal(t, s.Values[0].Message, "level=info msg=1")
			require.Equal(t, s.Values[1].Message, "level=info msg=2")
		} else {
			require.Lenf(t, s.Values, 1, "one Loki value in message %d expected", i+1)
			require.Equal(t, s.Values[0].Message, "level=info msg=3")
		}
	}
}

// TestSourceAttribute tests:
// - source attribute
func TestSourceAttribute(t *testing.T) {
	msgs, err := testInternal(sourceAttrTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 1, "one label expected")
	require.Contains(t, s.Stream, "source", `label "source" expected`)
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
// - all available logrus attributes as labels
func TestLabelsEnabled(t *testing.T) {
	msgs, err := testInternal(labelEnabledTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]

	labels := s.Stream
	require.Len(t, labels, 6, `expected 6 labels: "source", extra field "test", "time", "level", "func", "msg"`)

	for k, v := range labels {
		switch k {
		case "source":
			require.Equal(t, v, "test")
		case "test":
			require.Equal(t, v, "value")
		case "time":
			date, err := time.Parse(time.RFC3339Nano, v)
			require.NoError(t, err)

			since := time.Since(date)
			require.Less(t, since, 1*time.Second, "log time should be less than 1 second ago")
		case "level":
			require.Equal(t, v, logrus.InfoLevel.String())
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

	// Do not test formatter because is is implemented by logrus
}

// TestFormatter tests:
// - log message with a different formatter
func TestFormatter(t *testing.T) {
	msgs, err := testInternal(formatterTest)
	require.NoError(t, err)

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	v := m.Streams[0].Values
	require.Len(t, v, 1, "1 log value expected")

	var data map[string]string
	err = json.Unmarshal([]byte(v[0].Message), &data)
	require.NoError(t, err)

	require.Len(t, data, 3)
	require.Contains(t, data, "level")
	require.Contains(t, data, "msg")
	require.Contains(t, data, "time")
}

// TestRemoveColors tests:
// - removing colors from the message string
func TestRemoveColors(t *testing.T) {
	msgs, err := testInternal(removeColorTest)
	require.NoError(t, err)

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	v := m.Streams[0].Values
	require.Len(t, v, 1, "1 log value expected")

	require.Equal(t, "level=info msg=test", v[0].Message, "unexpected message")
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

	require.Equal(t, "level=warning msg=test", v[0].Message, "unexpected message")
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
		require.Equal(t, s.Values[0].Message, fmt.Sprintf("level=info msg=%d", i+1))
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
		require.Equal(t, s.Values[0].Message, fmt.Sprintf("level=info msg=%d", i+1))
	}
}

// TestErrors tests:
// - logged errors (error at creating the loki message)
func TestErrors(t *testing.T) {
	var out strings.Builder
	logrus.SetOutput(&out)

	msgs, err := testInternal(errorsTest)
	require.NoError(t, err)
	require.Len(t, msgs, 0, "no Loki message expected")

	output := strings.TrimSpace(out.String())
	require.Equal(t, `level=error msg="Failed to create loki value from entry: failed to create entry"`, output)

	logrus.SetOutput(io.Discard)
}

type errorFormatter struct{}

func (f *errorFormatter) Format(e *logrus.Entry) ([]byte, error) {
	return nil, errors.New("failed to create entry")
}

// TestErrors tests:
// - suppressed errors (error at creating the loki message)
func TestSuppressErrors(t *testing.T) {
	var out strings.Builder
	logrus.SetOutput(&out)

	msgs, err := testInternal(suppressErrorsTest)
	require.NoError(t, err)
	require.Len(t, msgs, 0, "no Loki message expected")

	output := strings.TrimSpace(out.String())
	require.Empty(t, "", output)

	logrus.SetOutput(io.Discard)
}

func getOptions(typ TestType) []Option {
	switch typ {
	case defaultTest, flushTest:
		return nil
	case sourceAttrTest:
		return []Option{WithSource("test")}
	case labelTest:
		return []Option{WithLabel("test", "test")}
	case labelEnabledTest:
		all := []Label{FieldsLabel, TimeLabel, LevelLabel, CallerLabel, MessageLabel}
		return []Option{WithSource("test"), WithLabelsEnabled(all...)}
	case formatterTest:
		return []Option{WithFormatter(&logrus.JSONFormatter{})}
	case removeColorTest:
		return []Option{WithRemoveColors(true)}
	case minLevelTest:
		return []Option{WithLevel(logrus.WarnLevel)}
	case flushWaitTest, batchIntervalTest:
		return []Option{WithBatchInterval(1 * time.Second)}
	case batchSizeTest:
		return []Option{WithBatchSize(2)}
	case synchronousTest:
		return []Option{WithSynchronous(true)}
	case errorsTest:
		return []Option{WithFormatter(&errorFormatter{})}
	case suppressErrorsTest:
		return []Option{WithSuppressErrors(true), WithFormatter(&errorFormatter{})}
	default:
		return nil
	}
}

func doLog(typ TestType, log *logrus.Logger, hook *Hook) {
	switch typ {
	case defaultTest:
		log.Trace("test")
		log.Debug("test")
		log.Info("test")
		log.Warn("test")
		log.Error("test")
	case flushTest:
		log.Info("1")
		log.Info("2")
		hook.Flush()
		log.Info("3")
	case sourceAttrTest, labelTest:
		log.Info("test")
	case labelEnabledTest:
		log.SetReportCaller(true)
		log.WithField("test", "value").Info("test")
	case formatterTest:
		log.Info("test")
	case removeColorTest:
		colored := fmt.Sprintf("\x1b[%dm%s\x1b[0m", 36, "test") // blue
		log.Info(colored)
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
	case suppressErrorsTest:
		log.Info("test")
	default:
		break
	}
}

func testInternal(typ TestType) ([]*lokiMessage, error) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.TraceLevel)

	var (
		messages  []*lokiMessage
		serverErr error
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, err := readLoki(w, r)
		if err != nil {
			serverErr = err
		} else {
			messages = append(messages, m)
		}
	}))

	defer server.Close()

	url := server.URL
	ops := getOptions(typ)
	hook := NewHook(url, ops...)

	logger.AddHook(hook)

	doLog(typ, logger, hook)

	hook.Close()

	if serverErr != nil {
		return nil, serverErr
	}

	return messages, nil
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
