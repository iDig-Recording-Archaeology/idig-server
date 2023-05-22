package main

import (
	"fmt"
	"sort"
	"testing"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

func TestAttachments(t *testing.T) {
	type TestCase struct {
		Op         string
		Name       string
		Checksum   string
		Data       string
		ShouldFail bool
	}

	testCases := []TestCase{
		{Op: "write", Name: "one", Checksum: "sum1", Data: "data1"},
		{Op: "write", Name: "one", Checksum: "sum2", Data: "data2"},
		{Op: "write", Name: "two", Checksum: "sum1", Data: "data1"},
		{Op: "read", Name: "one", Checksum: "sum1", Data: "data1"},
		{Op: "read", Name: "one", Checksum: "sum2", Data: "data2"},
		{Op: "read", Name: "one", Checksum: "sum3", Data: "data3", ShouldFail: true},
		{Op: "read", Name: "two", Checksum: "sum1", Data: "data1"},
		{Op: "read", Name: "three", Checksum: "sum1", Data: "data1", ShouldFail: true},
		{Op: "exists", Name: "one", Checksum: "sum1"},
		{Op: "exists", Name: "one", Checksum: "sum2"},
		{Op: "exists", Name: "one", Checksum: "sum3", ShouldFail: true},
		{Op: "exists", Name: "two", Checksum: "sum1"},
		{Op: "exists", Name: "three", Checksum: "sum1", ShouldFail: true},
	}

	b, err := NewMemoryBackend("test-user", "test-trench")
	if err != nil {
		t.Error(err)
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			switch tc.Op {
			case "write":
				err := b.WriteAttachment(tc.Name, tc.Checksum, []byte(tc.Data))
				if err != nil {
					t.Errorf("Test Case #%d: %s", i, err)
				}
			case "read":
				data, err := b.ReadAttachment(tc.Name, tc.Checksum)
				if err != nil {
					if tc.ShouldFail {
						return
					}
					t.Errorf("Test Case #%d: %s", i, err)
				} else if tc.ShouldFail {
					t.Errorf("Test Case #%d", i)
				}
				if string(data) != tc.Data {
					t.Errorf("Test Case #%d", i)
				}
			case "exists":
				if b.ExistsAttachment(tc.Name, tc.Checksum) == tc.ShouldFail {
					t.Errorf("Test Case #%d", i)
				}
			}
		})
	}
}

func TestTrench(t *testing.T) {
	b, err := NewMemoryBackend("test-user", "test-trench")
	assertNoError(t, err)

	assertEqual(t, b.Head(), "")

	surveys := generateSurveys(10)
	v, err := b.WriteTrench("test-dev", "", nil, surveys)
	assertNoError(t, err)

	assertEqual(t, b.Head(), v)

	versions, err := b.ListVersions()
	assertNoError(t, err)
	assertEqual(t, len(versions), 1)
	assertEqual(t, versions[0].Version, v)

	surveysAtHead, err := b.ReadSurveys()
	assertNoError(t, err)
	assertEqualSurveys(t, surveys, surveysAtHead)
}

func TestWritePreferences(t *testing.T) {
	b, err := NewMemoryBackend("test-user", "test-trench")
	assertNoError(t, err)

	surveys := generateSurveys(10)
	_, err = b.WriteTrench("test-dev", "", []byte("prefs-1"), surveys)
	assertNoError(t, err)

	preferences, err := b.ReadPreferences()
	assertNoError(t, err)
	assertEqual(t, string(preferences), "prefs-1")

	err = b.WritePreferences([]byte("prefs-2"))
	assertNoError(t, err)

	preferences, err = b.ReadPreferences()
	assertNoError(t, err)
	assertEqual(t, string(preferences), "prefs-2")

	// Make sure surveys were not affected
	surveysAtHead, err := b.ReadSurveys()
	assertNoError(t, err)
	assertEqualSurveys(t, surveys, surveysAtHead)
}

func assertNoError(t *testing.T, err error) {
	if err != nil {
		t.Error(err)
	}
}

func assertEqual[V comparable](t *testing.T, actual, expected V) {
	if actual != expected {
		t.Errorf("%v != %v", actual, expected)
	}
}

func assertEqualSurveys(t *testing.T, actual []Survey, expected []Survey) {
	actualMap := NewSurveyMap(actual)
	expectedMap := NewSurveyMap(expected)
	actualKeys := maps.Keys(actualMap)
	expectedKeys := maps.Keys(expectedMap)
	sort.Strings(actualKeys)
	sort.Strings(expectedKeys)
	slices.Equal(actualKeys, expectedKeys)
}

func generateSurveys(count int) []Survey {
	var surveys []Survey
	for i := 0; i < count; i++ {
		s := Survey{
			"IdentifierUUID": fmt.Sprintf("ID%03d", i),
			"Title":          fmt.Sprintf("Context %d", i),
			"Type":           "Context",
		}
		surveys = append(surveys, s)
	}
	return surveys
}
