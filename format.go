package loki

import (
	"bytes"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// logfmtFormatter is the default formatter for log entries sent to Loki and they are sent in logfmt format.
// The difference to logrus.TextFormatter is that some fields are disabled by default (e.g. timestamp) and
// values are quoted differently: the only characters that are escaped are `"` and `\`.
// Additionally, ANSI colors can be removed before quoting depending on the setting.
//
// The code is from logrus and has been highly modified.
type logfmtFormatter struct {
	name         string
	removeColors bool
}

// Format creates the logfmt string from the log entry.
func (f *logfmtFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b bytes.Buffer

	if f.name != "" {
		f.appendKeyValue(&b, "name", f.name)
	}
	f.appendKeyValue(&b, logrus.FieldKeyLevel, entry.Level.String())
	if entry.Message != "" {
		f.appendKeyValue(&b, logrus.FieldKeyMsg, entry.Message)
	}
	if entry.HasCaller() {
		funcVal, _ := callerPrettyfier(entry.Caller)

		if funcVal != "" {
			f.appendKeyValue(&b, logrus.FieldKeyFunc, funcVal)
		}
	}

	// Add extra fields
	data := make(logrus.Fields)
	for k, v := range entry.Data {
		data[k] = v
	}

	prefixFieldClashes(data, entry.HasCaller())

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		f.appendKeyValue(&b, key, data[key])
	}

	return b.Bytes(), nil
}

func prefixFieldClashes(data logrus.Fields, withCaller bool) {
	timeKey := logrus.FieldKeyTime
	if t, ok := data[timeKey]; ok {
		data["fields."+timeKey] = t
		delete(data, timeKey)
	}

	msgKey := logrus.FieldKeyMsg
	if m, ok := data[msgKey]; ok {
		data["fields."+msgKey] = m
		delete(data, msgKey)
	}

	levelKey := logrus.FieldKeyLevel
	if l, ok := data[levelKey]; ok {
		data["fields."+levelKey] = l
		delete(data, levelKey)
	}

	logrusErrKey := logrus.FieldKeyLogrusError
	if l, ok := data[logrusErrKey]; ok {
		data["fields."+logrusErrKey] = l
		delete(data, logrusErrKey)
	}

	if withCaller {
		funcKey := logrus.FieldKeyFunc
		if l, ok := data[funcKey]; ok {
			data["fields."+funcKey] = l
		}
	}
}

func (f *logfmtFormatter) appendKeyValue(b *bytes.Buffer, key string, value interface{}) {
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	b.WriteString(key)
	b.WriteByte('=')

	var strValue string
	if s, ok := value.(string); ok {
		strValue = s
	} else {
		strValue = fmt.Sprint(value)
	}
	if f.removeColors {
		strValue = removeColors(strValue)
	}

	b.WriteString(quoteIfNeeded(strValue))
}

// quoteIfNeeded adds quotation marks to the string if needed.
// Quoting is done if either space or '=' occurs.
// Only the characters `"` and `\` are escaped.
func quoteIfNeeded(s string) string {
	// check if string needs quoting or escaping
	quoting := len(s) == 0 || s[0] == '"' || s[len(s)-1] == '"'
	escape := false

	for _, c := range s {
		switch c {
		case ' ', '=':
			quoting = true
		case '"', '\\':
			escape = true
		}
		if quoting && escape {
			break
		}
	}

	// create the quoted / escaped string
	var b strings.Builder
	b.Grow((2*len(s))/3 + 2)

	if quoting {
		b.WriteByte('"')
	}
	if escape {
		for _, c := range s {
			switch c {
			case '"', '\\':
				b.WriteByte('\\')
				fallthrough
			default:
				b.WriteRune(c)
			}
		}
	} else {
		b.WriteString(s)
	}
	if quoting {
		b.WriteByte('"')
	}

	return b.String()
}

var colorParts = []string{
	"[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[-a-zA-Z\\d\\/#&.:=?%@~_]*)*)?\u0007)",
	"(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-ntqry=><~]))",
}
var colorRegex = regexp.MustCompile(strings.Join(colorParts, "|"))

// removeColors removes ANSI-colors in a string
func removeColors(s string) string {
	if colorRegex.MatchString(s) {
		return colorRegex.ReplaceAllString(s, "")
	}

	return s
}

// callerPrettyfier formats a func value in format ("file:line:func", "") (empty file value).
// This kind of creating the caller string is the most efficient way, e.g. compared to Sprintf.
// - this implementation: 258 ns/op, 67 B/op
// - Sprintf:             794 ns/op, 112 B/op
func callerPrettyfier(c *runtime.Frame) (string, string) {
	i := strings.LastIndexByte(c.Function, '.')

	var f strings.Builder
	f.Grow(len(c.File) + (len(c.Function) - i) + 10) // 10 extra needed

	f.WriteString(c.File)
	f.WriteByte(':')
	f.WriteString(strconv.Itoa(c.Line))
	f.WriteByte(':')

	if i >= 0 {
		f.WriteString(c.Function[i+1:])
	} else {
		f.WriteString(c.Function)
	}

	f.WriteString("()")
	return f.String(), ""
}
