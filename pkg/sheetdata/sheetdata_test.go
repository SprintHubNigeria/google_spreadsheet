package sheetdata

import (
	"reflect"
	"testing"
)

func TestFormatDateFromSheet(t *testing.T) {
	testCases := map[string]struct {
		Input          string
		ExpectedResult date
		Err            error
	}{
		"Accept dates in format YYYY-MM-DD":          {"2018-05-30", date{year: 2018, month: 05, day: 30}, nil},
		"Rejects date format YY-MM-DD":               {"18-05-30", date{year: 0, month: 0, day: 0}, errMalformedDate},
		"Accept dates in format DD/MM/YY":            {"30/05/18", date{year: 2018, month: 05, day: 30}, nil},
		"Date format DD-MM-YYYY returns wrong value": {"18/05/30", date{year: 2030, month: 05, day: 18}, nil},
	}

	for testCase, data := range testCases {
		got, err := formatDateFromSheet(data.Input)
		if !reflect.DeepEqual(data.ExpectedResult, got) {
			t.Errorf("%s: %s\nExpected %+v\nGot %+v\n", testCase, data.Input, data.ExpectedResult, got)
		}
		if data.Err != err {
			t.Errorf("Expected error value %v, got %v\n", data.Err, err)
		}
	}
}
