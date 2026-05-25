package utils

import "time"

func WeekDate(year, week int, day string, tz *time.Location) (time.Time, time.Time) {
	// Start from the middle of the year:
	t := time.Date(year, 7, 1, 0, 0, 0, 0, tz)
	var conclusionDate time.Weekday
	switch day {
	case time.Sunday.String(): // -1 day = Saturday
		conclusionDate = time.Saturday
	case time.Monday.String(): // -1 day = Sunday
		conclusionDate = time.Sunday
	case time.Tuesday.String(): // -1 day = Monday
		conclusionDate = time.Monday
	case time.Wednesday.String(): // -1 day = Tuesday
		conclusionDate = time.Tuesday
	case time.Thursday.String(): // -1 day = Wednesday
		conclusionDate = time.Wednesday
	case time.Friday.String(): // -1 day = Thursday
		conclusionDate = time.Thursday
	case time.Saturday.String(): // -1 day = Friday
		conclusionDate = time.Friday
	}
	// Roll back to Monday:
	if wd := t.Weekday(); wd == conclusionDate {
		t = t.AddDate(0, 0, -6)
	} else {
		t = t.AddDate(0, 0, -int(wd)+1)
	}

	// Difference in weeks:
	_, w := t.ISOWeek()
	t = t.AddDate(0, 0, (week-w)*7)
	e := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, tz)
	e = e.AddDate(0, 0, 6)
	return t, e
}
