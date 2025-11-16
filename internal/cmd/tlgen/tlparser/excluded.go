package tlparser

import "fmt"

var excludedDefinitions = map[string]null{
	"true":      {},
	"boolFalse": {},
	"boolTrue":  {},
	"vector":    {},

	"invokeAfterMsg":                {},
	"invokeAfterMsgs":               {},
	"initConnection":                {},
	"invokeWithLayer":               {},
	"invokeWithoutUpdates":          {},
	"invokeWithMessagesRange":       {},
	"invokeWithTakeout":             {},
	"invokeWithBusinessConnection":  {},
	"invokeWithGooglePlayIntegrity": {},
	"invokeWithApnsSecret":          {},
	"invokeWithReCaptcha":           {},
}

var excludedTypes = map[string]null{
	"int":    {},
	"long":   {},
	"double": {},
	"string": {},
	"bytes":  {},
}

type errExcluded struct {
	name string
}

func (e errExcluded) Error() string {
	return fmt.Sprintf("excluded: %s", e.name)
}
