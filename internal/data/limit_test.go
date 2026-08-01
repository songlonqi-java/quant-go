package data

import "testing"

func TestLimitFallbackUsesBoardSpecificThresholds(t *testing.T) {
	if IsApproxLimitUp("300001.SZ", 11, 10) {
		t.Fatal("10% rise on ChiNext must not be treated as a 20% limit-up")
	}
	if !IsApproxLimitUp("300001.SZ", 11.96, 10) {
		t.Fatal("ChiNext fallback should recognize an approximately 20% limit-up")
	}
	if IsApproxLimitUp("830001.BJ", 12, 10) {
		t.Fatal("20% rise on BSE must not be treated as a 30% limit-up")
	}
	if !IsApproxLimitDown("688001.SH", 8.04, 10) {
		t.Fatal("STAR fallback should recognize an approximately 20% limit-down")
	}
}

func TestExactLimitPricesDisableGenericFallback(t *testing.T) {
	bar := DailyBar{
		TsCode: "300001.SZ", RawOpen: 11, RawClose: 11,
		UpLimit: 12, DownLimit: 8,
	}
	if bar.IsLimitUpOpenWithFallback(10) {
		t.Fatal("exact 20% limit price says a 10% open remains tradable")
	}
}
