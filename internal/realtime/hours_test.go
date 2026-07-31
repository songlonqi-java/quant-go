package realtime

import (
	"testing"
	"time"
)

func TestIsAShareTradingHours(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before open", time.Date(2026, 7, 31, 9, 29, 0, 0, chinaLocation), false},
		{"morning", time.Date(2026, 7, 31, 9, 30, 0, 0, chinaLocation), true},
		{"lunch", time.Date(2026, 7, 31, 12, 0, 0, 0, chinaLocation), false},
		{"afternoon", time.Date(2026, 7, 31, 14, 59, 0, 0, chinaLocation), true},
		{"after close", time.Date(2026, 7, 31, 15, 1, 0, 0, chinaLocation), false},
		{"weekend", time.Date(2026, 8, 1, 10, 0, 0, 0, chinaLocation), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAShareTradingHours(tt.at); got != tt.want {
				t.Fatalf("IsAShareTradingHours(%s) = %t, want %t", tt.at, got, tt.want)
			}
		})
	}
}
