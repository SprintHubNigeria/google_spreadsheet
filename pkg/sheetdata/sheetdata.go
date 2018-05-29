package sheetdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// SheetEntry models the data we are interested in from the spreadsheet
type SheetEntry struct {
	FirstName string
	LastName  string
	Email     string
	EndDate   time.Time
}

// FullName returns the first name and the last name separated by a space
func (s SheetEntry) FullName() string {
	return s.FirstName + " " + s.LastName
}

// DaysLeft returns the number of days left until subscription expiry
func (s SheetEntry) DaysLeft() int {
	now := time.Now().UTC()
	return int(s.EndDate.Sub(now).Hours()) / 24
}

// NewSheetEntry constructs a SheetEntry from a row entry in a spreadsheet
func NewSheetEntry(data []interface{}) (SheetEntry, error) {
	// Parse the time from the response
	firstName, ok := data[0].(string)
	if !ok {
		return SheetEntry{}, errors.New("Unexpected first name value")
	}
	lastName, ok := data[1].(string)
	if !ok {
		return SheetEntry{}, errors.New("Unexpected last name value")
	}
	date, ok := data[4].(string)
	if !ok {
		return SheetEntry{}, errors.New("Unexpected date value")
	}
	email, ok := data[3].(string)
	if !ok || email == "" {
		return SheetEntry{}, errors.New("Unexpected email value " + email)
	}
	expiryDate, err := TimeFromSheet(date)
	if err != nil {
		return SheetEntry{}, errors.WithMessage(err, "Bad time value")
	}
	return SheetEntry{
		Email:     email,
		EndDate:   expiryDate,
		FirstName: firstName,
		LastName:  lastName,
	}, nil
}

type date struct {
	year, month, day int
}

// TimeFromSheet converts the time gotten from the spreadsheet to a time.Time value
// If there is a problem during the conversion, it returns a time.Time value with the default zero values
// and an error. If the conversion succeeds, it returns the converted time and no error.
func TimeFromSheet(date string) (time.Time, error) {
	dt, err := formatDateFromSheet(date)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := time.LoadLocation("")
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(dt.year, time.Month(dt.month), dt.day, 0, 0, 0, 0, loc).UTC(), nil
}

var errMalformedDate = fmt.Errorf("malformed date format: date should be in either YYYY-MM-DD or DD/MM/YY formats")

func formatDateFromSheet(dateString string) (date, error) {
	var (
		parsedDate = date{}
		d, m, y    int
		err        error
	)
	dateString = strings.Trim(dateString, " ")
	if strings.Index(dateString, "-") != -1 {
		if strings.Index(dateString, "-") < 4 {
			return parsedDate, errMalformedDate
		}
		dateString = strings.Replace(dateString, "-", "", -1)
		if _, err = fmt.Sscanf(dateString, "%4d%2d%2d", &y, &m, &d); err == nil {
			parsedDate.year = y
			parsedDate.month = m
			parsedDate.day = d
			return parsedDate, nil
		}
	} else if strings.Index(dateString, "/") != -1 {
		if strings.Index(dateString, "/") < 1 {
			return parsedDate, errMalformedDate
		}
		dateString = strings.Replace(dateString, "/", "", -1)
		if _, err = fmt.Sscanf(dateString, "%2d%2d%2d", &d, &m, &y); err == nil {
			y, err = strconv.Atoi("20" + strconv.Itoa(y))
			if err != nil {
				return parsedDate, errMalformedDate
			}
			parsedDate.year = y
			parsedDate.month = m
			parsedDate.day = d
			return parsedDate, nil
		}
	}
	return parsedDate, errMalformedDate
}
