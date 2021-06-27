package loki

// Option is the parameter type for options when initializing the log hook.
type Option interface {
	apply(h *Hook)
}
