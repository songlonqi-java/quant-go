package data

import "testing"

func TestGetFinaIndicatorAsOfUsesAnnouncementDate(t *testing.T) {
	store := NewFundamentalStore()
	store.LoadFinaIndicators([]FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: "20260430", EndDate: "20260331", Roe: 12},
		{TsCode: "000001.SZ", AnnDate: "20260830", EndDate: "20260630", Roe: 18},
	})

	fi, ok := store.GetFinaIndicatorAsOf("000001.SZ", "20260701")
	if !ok {
		t.Fatal("GetFinaIndicatorAsOf() ok = false, want true")
	}
	if fi.Roe != 12 {
		t.Fatalf("Roe = %.0f, want 12", fi.Roe)
	}
	fi, ok = store.GetFinaIndicatorAsOf("000001.SZ", "20260901")
	if !ok {
		t.Fatal("GetFinaIndicatorAsOf() ok = false, want true")
	}
	if fi.Roe != 18 {
		t.Fatalf("Roe = %.0f, want 18", fi.Roe)
	}
}
