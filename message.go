package loki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// Label is the enum type to define additional labels to be added to the Loki message.
type Label uint

const (
	// SourceLabel adds the source attribute
	SourceLabel Label = iota

	// FieldsLabel adds all extra fields as labels
	FieldsLabel

	// TimeLabel adds the time
	TimeLabel

	// LevelLabel adds the level
	LevelLabel

	// CallerLabel adds the caller which format "[file]:[line]:[function]"
	CallerLabel

	// MessageLabel adds the message as an extra label
	MessageLabel
)

type lokiLabels map[string]string

func (l lokiLabels) equals(o lokiLabels) bool {
	if len(l) != len(o) {
		return false
	}

	for k, v := range l {
		if v2, ok := o[k]; !ok || v != v2 {
			return false
		}
	}

	return true
}

func (h *Hook) lokiLabels(e *logrus.Entry) lokiLabels {
	l := lokiLabels{}

	for _, lbl := range h.labels {
		switch lbl {
		case SourceLabel:
			if h.srcAttr != "" && h.src != "" {
				l[h.srcAttr] = h.src
			}
		case FieldsLabel:
			for k, v := range e.Data {
				l[k] = fmt.Sprint(v)
			}
		case TimeLabel:
			l["time"] = e.Time.String()
		case LevelLabel:
			l["level"] = e.Level.String()
		case CallerLabel:
			if e.Caller != nil {
				l["call"] = fmt.Sprintf("%s:%d:%s", e.Caller.File, e.Caller.Line, e.Caller.Function)
			}
		case MessageLabel:
			l["message"] = e.Message
		}
	}

	return l
}

type lokiValue struct {
	Date    time.Time
	Message string
}

func (v *lokiValue) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`["`)
	b.WriteString(strconv.FormatInt(v.Date.UnixNano(), 10))
	b.WriteString(`",`)

	bytes, err := json.Marshal(v.Message)
	if err != nil {
		return nil, err
	}

	b.Write(bytes)
	b.WriteByte(']')

	return b.Bytes(), nil
}

func (h *Hook) lokiValue(e *logrus.Entry) (*lokiValue, error) {
	f := h.formatter
	if f == nil {
		if e.Logger == nil || e.Logger.Formatter == nil {
			return nil, errors.New("no formatter set")
		}

		f = e.Logger.Formatter
	}

	bytes, err := f.Format(e)
	if err != nil {
		return nil, err
	}

	s := string(bytes)
	if h.removeColors {
		s = removeColors(s)
	}

	v := &lokiValue{
		Date:    e.Time,
		Message: s,
	}

	return v, nil
}

type lokiStream struct {
	Stream lokiLabels   `json:"stream"`
	Values []*lokiValue `json:"values"`
}

type lokiMessage struct {
	Streams []*lokiStream `json:"streams"`
}
