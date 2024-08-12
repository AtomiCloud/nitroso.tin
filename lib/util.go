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
