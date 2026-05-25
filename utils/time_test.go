package utils_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hexlinevault/core-api.git/utils"
)

func TestISOWeekDay(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Bangkok")
	now := time.Now().In(loc)
	fmt.Println("now", now)
	year, week := now.ISOWeek()
	fmt.Println("Year/Week", year, week)
	start, end := utils.WeekDate(year, week, "Monday", loc)
	fmt.Println(start, end)
}
