package sheetdata

import (
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
	date, ok := data[5].(string)
	if !ok {
		return SheetEntry{}, errors.New("Unexpected date value")
	}
	email, ok := data[3].(string)
	if !ok {
		return SheetEntry{}, errors.New("Unexpected email value")
	}
	now := time.Now().UTC()
	expiryDate, err := TimeFromSheet(date, now)
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

// TimeFromSheet converts the time gotten from the spreadsheet to a time.Time value
// If there is a problem during the conversion, it returns a time.Time value with the default zero values
// and an error. If the conversion succeeds, it returns the converted time and no error.
func TimeFromSheet(date string, now time.Time) (time.Time, error) {
	expires := strings.Split(date, "/")
	y, err := strconv.Atoi("20" + expires[2])
	if err != nil {
		return time.Time{}, err
	}
	mInt, err := strconv.Atoi(expires[1])
	if err != nil {
		return time.Time{}, err
	}
	m := time.Month(mInt)
	d, err := strconv.Atoi(expires[0])
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).UTC(), nil
}
