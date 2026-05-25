package utils

import (
	"strings"
)

func PrintTable(data [][]string) string {
	// Calculate the maximum width for each column
	columnWidths := make([]int, len(data[0]))
	for _, row := range data {
		for i, cell := range row {
			if len(cell) > columnWidths[i] {
				columnWidths[i] = len(cell)
			}
		}
	}

	// Print the top border
	str := PrintBorder(columnWidths)

	// Print each row of data
	for _, row := range data {
		str += PrintRow(row, columnWidths)
		str += PrintBorder(columnWidths)
	}

	str += "\n"
	return str
}

func PrintBorder(columnWidths []int) string {
	str := ""
	for _, width := range columnWidths {
		str += "+"
		str += strings.Repeat("-", width+2)
	}
	str += "+\n"
	return str
}

func PrintRow(row []string, columnWidths []int) string {
	str := ""
	for i, cell := range row {
		str += "| "
		str += cell
		str += strings.Repeat(" ", columnWidths[i]-len(cell)+1)
	}
	str += "|\n"
	return str
}
