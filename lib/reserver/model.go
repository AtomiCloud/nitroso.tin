package reserver

import (
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
)

type Count map[string]map[string]map[string]int

type LoginStore struct {
	UserData string
	Find     enricher.FindStore
}
