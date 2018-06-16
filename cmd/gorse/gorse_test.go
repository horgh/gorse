package main

import "testing"

func TestSubstr(t *testing.T) {
	tests := []struct {
		Input  string
		Length int
		Output string
	}{
		{"", 1, ""},
		{"hi", 1, "h"},
		{"hi", 2, "hi"},
		{"hi", 3, "hi"},
		{"☃", 1, "☃"},
		{"☃", 2, "☃"},
		{"☃", 20, "☃"},
		{"☃☃", 1, "☃"},
		{"☃☃", 2, "☃☃"},
		{"☃☃", 3, "☃☃"},
		{"☃h", 1, "☃"},
		{"☃h", 2, "☃h"},
		{"☃h", 3, "☃h"},
		{"☃h☃", 3, "☃h☃"},
		{"h☃h☃", 1, "h"},
		{"h☃h☃", 2, "h☃"},
		{"h☃h☃", 3, "h☃h"},
		{"h☃h☃", 4, "h☃h☃"},
	}

	for _, test := range tests {
		output := substr(test.Input, test.Length)
		if output == test.Output {
			continue
		}
		t.Errorf("substring(%s, %d) = %s, wanted %s", test.Input, test.Length,
			output, test.Output)
	}
}
