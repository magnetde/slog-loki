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
	// FieldsLabel adds all extra fields as labels
	FieldsLabel Label = iota

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
	l := make(lokiLabels, len(h.labels))
	for k, v := range h.labels {
		l[k] = fmt.Sprint(v)
	}

	for _, lbl := range h.labelsEnabled {
		switch lbl {
		case FieldsLabel:
			for k, v := range e.Data {
				l[k] = fmt.Sprint(v)
			}
		case TimeLabel:
			l["time"] = e.Time.Format(time.RFC3339Nano)
		case LevelLabel:
			l["level"] = e.Level.String()
		case CallerLabel:
			if e.HasCaller() {
				function, _ := callerPrettyfier(e.Caller)
				l["func"] = function
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

				v.Date = time.Unix(0, ns)
			case 1:
				v.Message = val
			default:
				return fmt.Errorf("unexpected value at array index %d", arrIdx)
			}

			arrIdx++
		}
	}

	return nil
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

	v := &lokiValue{
		Date:    e.Time,
		Message: string(bytes),
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
