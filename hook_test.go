package loki

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kr/logfmt"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type TestType int

const (
	// see TestLokiHook
	defaultTest TestType = iota

	// see TestFlush
	flushTest

	// see TestSource
	sourceAttrTest
)

// TestLokiHook tests:
// - different levels
// - labels at default settings
// - increasing dates at loki values
func TestLokiHook(t *testing.T) {
	msgs, err := testInternal(defaultTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")

	m := msgs[0]
	require.Len(t, m.Streams, 1, "one Loki stream expected")

	s := m.Streams[0]
	require.Len(t, s.Stream, 1, "one label expected")
	require.Contains(t, s.Stream, "source", `label "source" expected`)

	v := s.Values
	require.Len(t, v, 5, "5 log values expected")

	var last time.Time
	for i, val := range v {
		expectlv := log.AllLevels[len(log.AllLevels)-1-i]
		expected := fmt.Sprintf("level=%s msg=test\n", expectlv.String())

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

	require.Len(t, msgs, 2, "2 Loki messages expected")

	for i, m := range msgs {
		require.Lenf(t, m.Streams, 1, "one Loki stream in message %d expected", i+1)

		s := m.Streams[0]
		if i == 0 {
			require.Lenf(t, s.Values, 2, "2 Loki values in message %d expected", i+1)

			require.Equal(t, s.Values[0].Message, "level=info msg=1\n")
			require.Equal(t, s.Values[1].Message, "level=info msg=2\n")
		} else {
			require.Lenf(t, s.Values, 1, "one Loki value in message %d expected", i+1)
			require.Equal(t, s.Values[0].Message, "level=info msg=3\n")
		}
	}

	v1 := msgs[0].Streams[0].Values[1].Date // 2. value in 1. message
	v2 := msgs[1].Streams[0].Values[0].Date // 1. value in 2. message

	require.True(t, v1.Before(v2), "logs should have a monotonously increasing timestamp")
}

// TestSourceAttribute tests:
// - different source attribute
func TestSourceAttribute(t *testing.T) {
	msgs, err := testInternal(sourceAttrTest)
	require.NoError(t, err)

	require.Len(t, msgs, 1, "one Loki message expected")
}

func getOptions(typ TestType) []Option {
	switch typ {
	case defaultTest, flushTest:
		return nil
	case sourceAttrTest:
		return []Option{WithSourceAttribute("src")}
	default:
		return nil
	}
}

func doLog(typ TestType, log *log.Logger, hook *Hook) {
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
	case sourceAttrTest:
		log.Info("test")
	default:
		break
	}
}

func testInternal(typ TestType) ([]*lokiMessage, error) {
	logger := log.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.TraceLevel)

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

	opts := getOptions(typ)
	hook, err := NewHook("test", url, opts...)
	if err != nil {
		return nil, err
	}

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

type ParsedLog struct {
	level log.Level
	time  time.Time
	msg   string
	data  map[string]string
}

func (p *ParsedLog) HandleLogfmt(key, val []byte) error {
	k := string(key)
	v := string(val)

	switch k {
	case "level":
		l, err := log.ParseLevel(v)
		if err != nil {
			return nil
		}

		p.level = l
	case "time":
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return nil
		}

		p.time = t
	case "msg":
		p.msg = v
	default:
		p.data[k] = v
	}

	return nil
}

func parseLog(line string) *ParsedLog {
	var data ParsedLog

	if err := logfmt.Unmarshal([]byte(line), &data); err != nil {
		return nil
	}

	return &data
}
