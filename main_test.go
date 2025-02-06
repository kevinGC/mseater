package main

import (
	"flag"
	"testing"
	"time"

	"github.com/kevinGC/mseater/crawler"
)

type testCase struct {
	name        string
	input       string
	expectError bool
}

func TestDate(t *testing.T) {
	tcs := []testCase{{
		name:  "good",
		input: "11-15",
	}, {
		name:        "contains good",
		input:       "11-15 n'stuff",
		expectError: true,
	}, {
		name:        "too detailed",
		input:       "11-15-2001",
		expectError: true,
	}, {
		name:        "too brief",
		input:       "11",
		expectError: true,
	}, {
		name:        "words",
		input:       "November 15 2001",
		expectError: true,
	}, {
		name:        "nonsense",
		input:       "소원을 말해봐",
		expectError: true,
	}}

	testFlag[date](t, tcs)
}

func TestZip(t *testing.T) {
	tcs := []testCase{{
		name:  "good",
		input: "48104",
	}, {
		name:        "contains good",
		input:       "48104z",
		expectError: true,
	}, {
		name:  "leading zero",
		input: "01778",
	}, {
		name:        "too long",
		input:       "012345",
		expectError: true,
	}, {
		name:        "too short",
		input:       "0123",
		expectError: true,
	}, {
		name:        "not numbers",
		input:       "corum",
		expectError: true,
	}}

	testFlag[zip](t, tcs)
}

// Awkward testing cludge. Blame to:
// https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#pointer-method-example.
//
// In short this says: a testValue[T] is a pointer to type T that implements
// flag.Value. I.e. a wordy way of saying it's a flag.Value.
type testValue[T any] interface {
	flag.Value
	*T
}

func testFlag[T any, PT testValue[T]](t *testing.T, tcs []testCase) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var concrete T
			customFlag := PT(&concrete)
			if err := customFlag.Set(tc.input); err != nil {
				if tc.expectError {
					return
				}
				t.Fatalf("%q could not be parsed: %v", tc.input, err)
			}
			if tc.expectError {
				t.Fatalf("expected error, but none found")
			}
			if customFlag.String() != tc.input {
				t.Fatalf("%q was parsed as %q", tc.input, customFlag.String())
			}
		})
	}

}

// TestPrintShowings doesn't really test anything, it just gives example
// output. Test with `-v` to see output.
func TestPrintShowings(t *testing.T) {
	when := func(clocktime string) time.Time {
		tim, err := time.Parse("15:04", clocktime)
		if err != nil {
			t.Fatal(err)
		}
		return tim
	}
	result := crawler.Result{
		Showings: []crawler.Showing{
			{
				Link:    "https://google.com",
				Theater: "Google",
				When:    when("7:11"),
			},
			{
				Link:    "https://mgoblog.com",
				Theater: "MGoBlog",
				When:    when("4:20"),
			},
			{
				Link:    "https://www.reddit.com/r/VWIDBuzz/",
				Theater: "VW ID.Buzz",
				When:    when("9:15"),
			},
		},
	}
	t.Logf("\n%s", formatShowings(result.Showings, false /* printLinks */))
	t.Logf("=======")
	t.Logf("\n%s", formatShowings(result.Showings, true /* printLinks */))
}
