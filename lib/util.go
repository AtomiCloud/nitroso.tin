package lib

import (
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
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

func StoreToPublic(store enricher.FindStore) map[string]map[string][]string {
	public := make(map[string]map[string][]string)
	for dir, dirStore := range store {
		public[dir] = make(map[string][]string)
		for date, dateStore := range dirStore {
			public[dir][date] = make([]string, 0)
			for tt := range dateStore {
				public[dir][date] = append(public[dir][date], tt)
			}
		}
	}
	return public
}
