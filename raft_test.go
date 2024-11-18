package liferaft

import (
	"slices"
	"testing"
)

func TestAppendNewEntries(t *testing.T) {
	cases := []struct {
		log, newEntries, expected []int
		newIdx                    int
	}{
		{
			log:        []int{1, 2, 3, 4, 5},
			newEntries: []int{4, 5, 6, 7, 8},
			newIdx:     3,
			expected:   []int{1, 2, 3, 4, 5, 6, 7, 8},
		},
		{
			log:        []int{1, 2, 3, 4, 5},
			newEntries: []int{4, 5},
			newIdx:     3,
			expected:   []int{1, 2, 3, 4, 5},
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			newLog := appendNewEntries(c.log, c.newIdx, c.newEntries)
			if !slices.Equal(c.expected, newLog) {
				t.Errorf("expected %v, got %v", c.expected, newLog)
			}
		})
	}
}
