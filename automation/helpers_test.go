package automation_test

import (
	"fmt"

	"github.com/mj41/go-mc26/data/registryid"
)

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}
