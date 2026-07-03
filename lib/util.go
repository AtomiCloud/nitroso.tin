package lib

import (
	"strings"
)

func ZincToHeliumDate(date string) string {
	frag := strings.Split(date, "-")
	return strings.Join([]string{frag[2], frag[1], frag[0]}, "-")
}

func HeliumToZincDate(date string) string {
	frag := strings.Split(date, "-")
	return strings.Join([]string{frag[2], frag[1], frag[0]}, "-")
}

// Deref returns the pointed-to value, or the zero value for a nil pointer —
// for reading the generated zinc SDK's optional (pointer) fields.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
