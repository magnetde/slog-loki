package loki

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

type lokiLabels map[string]string

func (l lokiLabels) Equals(o lokiLabels) bool {
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

type lokiMessage struct {
	Streams []*lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream lokiLabels   `json:"stream"`
	Values []*lokiValue `json:"values"`
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

func (h *Hook) lokiLabels(e *logrus.Entry) lokiLabels {
	l := lokiLabels{
		h.typeAttr: h.typ,
	}

	return l
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
