package interp

import (
	"context"
	"encoding/gob"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

// DatePart is a date-part argument of the date and currentdate tests
// (RFC 5260, section 4.2).
type DatePart string

const (
	DatePartYear    DatePart = "year"
	DatePartMonth   DatePart = "month"
	DatePartDay     DatePart = "day"
	DatePartDate    DatePart = "date"
	DatePartJulian  DatePart = "julian"
	DatePartHour    DatePart = "hour"
	DatePartMinute  DatePart = "minute"
	DatePartSecond  DatePart = "second"
	DatePartTime    DatePart = "time"
	DatePartISO8601 DatePart = "iso8601"
	DatePartStd11   DatePart = "std11"
	DatePartZone    DatePart = "zone"
	DatePartWeekday DatePart = "weekday"
)

func validDatePart(p string) bool {
	switch DatePart(strings.ToLower(p)) {
	case DatePartYear, DatePartMonth, DatePartDay, DatePartDate, DatePartJulian,
		DatePartHour, DatePartMinute, DatePartSecond, DatePartTime,
		DatePartISO8601, DatePartStd11, DatePartZone, DatePartWeekday:
		return true
	}
	return false
}

// floorDiv divides rounding towards negative infinity.
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// formatDatePart returns the value of a date-part for t, already shifted to the
// time zone the test asks for. The second result is false for an unknown part.
func formatDatePart(t time.Time, part string) (string, bool) {
	switch DatePart(strings.ToLower(part)) {
	case DatePartYear:
		return fmt.Sprintf("%04d", t.Year()), true
	case DatePartMonth:
		return fmt.Sprintf("%02d", int(t.Month())), true
	case DatePartDay:
		return fmt.Sprintf("%02d", t.Day()), true
	case DatePartDate:
		return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day()), true
	case DatePartJulian:
		// Days since 1858-11-17 of the calendar date in the selected zone.
		// Counted in Unix days, not time.Duration, which saturates after
		// about 292 years. 40587 is the MJD of 1970-01-01.
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		mjd := floorDiv(day.Unix(), 86400) + 40587
		return strconv.FormatInt(mjd, 10), true
	case DatePartHour:
		return fmt.Sprintf("%02d", t.Hour()), true
	case DatePartMinute:
		return fmt.Sprintf("%02d", t.Minute()), true
	case DatePartSecond:
		return fmt.Sprintf("%02d", t.Second()), true
	case DatePartTime:
		return fmt.Sprintf("%02d:%02d:%02d", t.Hour(), t.Minute(), t.Second()), true
	case DatePartISO8601:
		// RFC 3339 date-time, with "Z" for a zero offset.
		return t.Format("2006-01-02T15:04:05Z07:00"), true
	case DatePartStd11:
		return t.Format("Mon, 02 Jan 2006 15:04:05 -0700"), true
	case DatePartZone:
		// "+hhmm"/"-hhmm"; a zero offset is "+0000".
		return t.Format("-0700"), true
	case DatePartWeekday:
		return strconv.Itoa(int(t.Weekday())), true
	}
	return "", false
}

// parseZone parses a :zone argument, "+hhmm" or "-hhmm" (RFC 5260, section 4.1).
func parseZone(zone string) (*time.Location, error) {
	if len(zone) != 5 || (zone[0] != '+' && zone[0] != '-') {
		return nil, fmt.Errorf("invalid time zone %q: expected +hhmm or -hhmm", zone)
	}
	for _, c := range zone[1:] {
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("invalid time zone %q: expected +hhmm or -hhmm", zone)
		}
	}
	hours, _ := strconv.Atoi(zone[1:3])
	minutes, _ := strconv.Atoi(zone[3:5])
	if minutes > 59 {
		return nil, fmt.Errorf("invalid time zone %q: minutes out of range", zone)
	}
	offset := hours*3600 + minutes*60
	if zone[0] == '-' {
		offset = -offset
	}
	return time.FixedZone(zone, offset), nil
}

// parseHeaderDate extracts an RFC 2822 date-time from a header field value:
// the whole value (as in Date:) or the part after the last semicolon (as in
// Received:). It reports false for a value without a valid date-time,
// including dates that don't exist in the calendar.
func parseHeaderDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if t, err := mail.ParseDate(value); err == nil {
		return t, true
	}
	if i := strings.LastIndexByte(value, ';'); i != -1 {
		if t, err := mail.ParseDate(strings.TrimSpace(value[i+1:])); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// shiftDate returns t in the zone a date or currentdate test selects: its own
// (:originalzone), the :zone offset, or the local time zone. It reports false
// if a :zone value that came from a variable is not a valid offset.
func shiftDate(d *RuntimeData, t time.Time, zone string, originalZone bool) (time.Time, bool) {
	if originalZone {
		return t, true
	}
	if zone != "" {
		loc, err := parseZone(expandVars(d, zone))
		if err != nil {
			return time.Time{}, false
		}
		return t.In(loc), true
	}
	return t.In(d.location()), true
}

// DateTest implements the date test (RFC 5260, section 4), with the :index
// and :last arguments of the index extension (section 6).
type DateTest struct {
	MatcherTest

	// Zone is the :zone argument, "" if absent.
	Zone string
	// OriginalZone is set for :originalzone.
	OriginalZone bool

	Header   string
	DatePart string

	// Index selects the Index-th field named Header (1-based); 0 means the
	// first. With Last, fields are counted from the end.
	Index int
	Last  bool
}

func (t DateTest) Check(_ context.Context, d *RuntimeData) (bool, error) {
	values, err := d.Msg.HeaderGet(expandVars(d, t.Header))
	if err != nil {
		return false, err
	}

	index := t.Index
	if index == 0 {
		index = 1 // only the first field is used without :index
	}
	values = selectIndexed(values, index, t.Last)

	var date time.Time
	valid := false
	if len(values) == 1 {
		date, valid = parseHeaderDate(values[0])
	}

	if t.isCount() {
		count := uint64(0)
		if valid {
			count = 1
		}
		return t.countMatches(d, count), nil
	}
	if !valid {
		return false, nil
	}

	return t.matchDate(d, date)
}

func (t DateTest) matchDate(d *RuntimeData, date time.Time) (bool, error) {
	shifted, ok := shiftDate(d, date, t.Zone, t.OriginalZone)
	if !ok {
		return false, nil
	}
	value, ok := formatDatePart(shifted, expandVars(d, t.DatePart))
	if !ok {
		return false, nil
	}
	return t.MatcherTest.tryMatch(d, value)
}

// CurrentDateTest implements the currentdate test (RFC 5260, section 5).
type CurrentDateTest struct {
	MatcherTest

	// Zone is the :zone argument, "" for the local time zone.
	Zone     string
	DatePart string
}

func (t CurrentDateTest) Check(_ context.Context, d *RuntimeData) (bool, error) {
	if t.isCount() {
		return t.countMatches(d, 1), nil
	}

	date := DateTest{
		MatcherTest: t.MatcherTest,
		Zone:        t.Zone,
		DatePart:    t.DatePart,
	}
	return date.matchDate(d, d.currentTime())
}

// selectIndexed applies the :index and :last arguments (RFC 5260, section 6)
// to the field values of a test: with index > 0 it returns only the
// index-th value (counting from the end with last), or none if there are
// fewer values. With index == 0 it returns values unchanged.
func selectIndexed(values []string, index int, last bool) []string {
	if index <= 0 {
		return values
	}
	i := index - 1
	if last {
		i = len(values) - index
	}
	if i < 0 || i >= len(values) {
		return nil
	}
	return values[i : i+1]
}

func init() {
	gob.Register(DateTest{})
	gob.Register(CurrentDateTest{})
}
