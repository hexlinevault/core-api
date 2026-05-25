package utils_test

import (
	"fmt"
	"testing"

	"github.com/hexlinevault/core-api.git/utils"
)

func TestPrintTable(t *testing.T) {
	data := [][]string{
		{"Username", "Profit"},
		{"a", "719,638,022.13"},
		{"b", "393,242,998.45"},
		{"c", "209,078,745.00"},
		{"d", "166,891,636.28"},
		{"e", "132,130,010.40"},
	}

	fmt.Println(utils.PrintTable(data))
}
