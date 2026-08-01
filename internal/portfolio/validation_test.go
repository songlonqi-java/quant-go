package portfolio

import (
	"strings"
	"testing"
)

func TestValidateLedgerRejectsHistoricalOversell(t *testing.T) {
	ledger := &Ledger{Transactions: []Transaction{
		{Date: "20260102", Code: "000001.SZ", Action: "sell", Shares: 100, Price: 11},
		{Date: "20260103", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10},
	}}
	err := ValidateLedger(ledger)
	if err == nil || !strings.Contains(err.Error(), "卖出") {
		t.Fatalf("ValidateLedger() error = %v, want oversell", err)
	}
}

func TestValidateLedgerAcceptsNormalizedValidTransactions(t *testing.T) {
	ledger := &Ledger{Transactions: []Transaction{
		{Date: "20260102", Code: "000001.SZ", Action: "buy", Shares: 200, Price: 10},
		{Date: "20260103", Code: "000001.SZ", Action: "sell", Shares: 100, Price: 11},
	}}
	if err := ValidateLedger(ledger); err != nil {
		t.Fatalf("ValidateLedger() error = %v", err)
	}
}

func TestValidateTransactionRejectsInvalidFields(t *testing.T) {
	tests := []Transaction{
		{Date: "20260230", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10},
		{Date: "20260102", Code: "000001", Action: "buy", Shares: 100, Price: 10},
		{Date: "20260102", Code: "000001.SZ", Action: "hold", Shares: 100, Price: 10},
		{Date: "20260102", Code: "000001.SZ", Action: "buy", Shares: 0.5, Price: 10},
		{Date: "20260102", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 0},
	}
	for _, transaction := range tests {
		if err := ValidateTransaction(transaction); err == nil {
			t.Errorf("ValidateTransaction(%+v) error = nil", transaction)
		}
	}
}
