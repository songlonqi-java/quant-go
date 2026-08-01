package data

import "testing"

func TestStoreFingerprintsAreOrderIndependentAndDataSensitive(t *testing.T) {
	left := NewMoneyflowStore([]Moneyflow{
		{TsCode: "000002.SZ", TradeDate: "20260102", NetMfAmount: 2},
		{TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 1},
	})
	right := NewMoneyflowStore([]Moneyflow{
		{TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 1},
		{TsCode: "000002.SZ", TradeDate: "20260102", NetMfAmount: 2},
	})
	if left.Fingerprint() != right.Fingerprint() {
		t.Fatal("moneyflow fingerprint must not depend on load order")
	}
	changed := NewMoneyflowStore([]Moneyflow{{TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 3}})
	if left.Fingerprint() == changed.Fingerprint() {
		t.Fatal("moneyflow data change must alter fingerprint")
	}

	fundA := NewFundamentalStore()
	fundA.LoadDailyBasics([]DailyBasic{{TsCode: "000001.SZ", TradeDate: "20260101", Pe: 10}})
	fundB := NewFundamentalStore()
	fundB.LoadDailyBasics([]DailyBasic{{TsCode: "000001.SZ", TradeDate: "20260101", Pe: 11}})
	if fundA.Fingerprint() == fundB.Fingerprint() {
		t.Fatal("fundamental data change must alter fingerprint")
	}
}
