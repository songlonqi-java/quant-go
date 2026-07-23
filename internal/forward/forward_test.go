package forward

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
)

func TestValidateStopsAfterInvalidBreakPrevLow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{
		{
			"signal_date": "20260101",
			"target_date": "20260102",
			"rank":        "1",
			"code":        "000001.SZ",
			"name":        "平安银行",
			"close":       "10.00",
			"status":      "pending",
		},
	}); err != nil {
		t.Fatalf("writeRows() error = %v", err)
	}

	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.5, 10),
		forwardBar("20260102", 10.1, 10.3, 9.4, 10.2),
		forwardBar("20260103", 10.2, 10.6, 10.1, 10.5),
		forwardBar("20260104", 10.5, 11.0, 10.3, 10.8),
		forwardBar("20260105", 10.8, 11.2, 10.6, 11.0),
		forwardBar("20260106", 11.0, 11.4, 10.8, 11.2),
	}

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	rows, err := readRows(path)
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["status"] != "invalid_break_prev_low" {
		t.Fatalf("status = %q, want invalid_break_prev_low", row["status"])
	}
	if row["day3_close"] != "" || row["day5_close"] != "" {
		t.Fatalf("invalid trade should not have day3/day5 returns: row=%v", row)
	}
}

func forwardBar(date string, open, high, low, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    "000001.SZ",
		TradeDate: date,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Vol:       1000,
		RawOpen:   open,
		RawHigh:   high,
		RawLow:    low,
		RawClose:  close,
		AdjFactor: 1,
	}
}
