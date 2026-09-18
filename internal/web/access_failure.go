package web

// accessFailure is a small local error for observe route guards; it maps to
// the shared writeHumanControlError JSON shape.
type accessFailure struct {
	status int
	code   string
}

func (e *accessFailure) Error() string { return e.code }
