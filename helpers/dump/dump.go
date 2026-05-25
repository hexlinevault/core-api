package dump

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/davecgh/go-spew/spew"
)

// DD -
func DD(d ...interface{}) {
	if _, file, no, ok := runtime.Caller(1); ok {
		fmt.Printf("\033[35;1mcalled from %s, line: %d\033[0m", file, no)
		for _, str := range strings.Split(spew.Sdump(d), "\n") {
			fmt.Printf("\n\033[35;1m%s\033[0m", str)
		}
	}
}
