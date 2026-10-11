// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package xsd

import "testing"

// Range facets order the partial date types and durations (durations
// only partially: P1M and P30D are incomparable).
func TestRangeFacetsOnPartialDatesAndDurations(t *testing.T) {
	for _, c := range []struct {
		prim, a, b string
		want       int
		ok         bool
	}{
		{"gYear", "2001", "2002", -1, true},
		{"gYear", "-0001", "0001", -1, true},
		{"gYear", "12345", "2001", 1, true},
		{"gYearMonth", "2001-12", "2002-01", -1, true},
		{"gMonthDay", "--02-29", "--03-01", -1, true},
		{"gDay", "---31", "---01", 1, true},
		{"gMonth", "--05", "--05", 0, true},
		{"gYear", "2001+14:00", "2000-10:00", 1, true},
		{"dateTime", "2001-01-01T24:00:00Z", "2001-01-02T00:00:00Z", 0, true},
		{"duration", "P1Y", "P12M", 0, true},
		{"duration", "PT36H", "P1D", 1, true},
		{"duration", "P1M", "P30D", 0, false},
		{"duration", "-P1D", "PT1H", -1, true},
		{"duration", "P1DT0.5S", "PT86400S", 1, true},
	} {
		got, ok := compare(c.prim, c.a, c.b)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%s %s vs %s: %d %v, want %d %v", c.prim, c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}
