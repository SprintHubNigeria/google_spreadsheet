package main

import "testing"

func TestShouldSendEmail(t *testing.T)  {
	testCases := map[string]struct {
		Value          int
		ExpectedResult bool
	}{
		"Zero should be false": {Value: 0, ExpectedResult: false},
		"Equal to 1 should be true": {Value: 1, ExpectedResult: true},
		"Equal to 3 should be true": {Value: 3, ExpectedResult: true},
		"Equal to 7 should be true": {Value: 7, ExpectedResult: true},
		"Negative number should be false": {Value: -1, ExpectedResult: false},
		"Any other number should be false": {Value: 20, ExpectedResult: false},
	}
	for testcase, data := range testCases {
		got := shouldSendEmail(data.Value)
		if got != data.ExpectedResult {
			t.Errorf("%s\n\tExpected: %v, Got: %v\n", testcase, data.ExpectedResult, got)
		}
	}
}