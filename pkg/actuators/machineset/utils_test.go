/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package machineset

import (
	"strconv"
	"testing"
)

func TestParseMemory(t *testing.T) {
	expectedResultInMiB := int64(3.75 * 1024)
	tests := []struct {
		input  string
		expect int64
	}{
		{
			input:  "3.75 GiB",
			expect: expectedResultInMiB,
		},
		{
			input:  "3.75 Gib",
			expect: expectedResultInMiB,
		},
		{
			input:  "3.75GiB",
			expect: expectedResultInMiB,
		},
		{
			input:  "3.75",
			expect: expectedResultInMiB,
		},
	}

	for _, test := range tests {
		got := parseMemory(test.input)
		if test.expect != got {
			t.Errorf("Memory parsing is incorrect, expected '%v', got '%v'", test.expect, got)
		}
	}
}

func TestParseCPU(t *testing.T) {
	tests := []struct {
		input  string
		expect int64
	}{
		{
			input:  strconv.FormatInt(8, 10),
			expect: int64(8),
		},
	}

	for _, test := range tests {
		got := parseCPU(test.input)
		if test.expect != got {
			t.Errorf("CPU parsing is incorrect, expected '%v', got '%v'", test.expect, got)
		}
	}
}
