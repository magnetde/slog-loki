package loki

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

var defaultFormatter = &logrus.TextFormatter{DisableTimestamp: true, DisableColors: true, CallerPrettyfier: callerPrettyfier}

type noColorFormatter struct {
	formatter logrus.Formatter
}

func (f *noColorFormatter) Format(e *logrus.Entry) ([]byte, error) {
	e.Message = removeColors(e.Message)
	return f.formatter.Format(e)
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
