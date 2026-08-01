package portfolio

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var stockCodePattern = regexp.MustCompile(`^[0-9]{6}\.(SH|SZ|BJ)$`)

// ValidateTransaction validates the portable transaction fields shared by
// YAML, CLI and Web storage. Storage-specific metadata is validated by its
// owning package.
func ValidateTransaction(transaction Transaction) error {
	if _, err := time.Parse("20060102", transaction.Date); err != nil {
		return fmt.Errorf("交易日期必须是有效的 YYYYMMDD 日期")
	}
	code := strings.ToUpper(strings.TrimSpace(transaction.Code))
	if !stockCodePattern.MatchString(code) {
		return fmt.Errorf("股票代码必须类似 600000.SH、000001.SZ 或 430047.BJ")
	}
	if transaction.Action != "buy" && transaction.Action != "sell" {
		return fmt.Errorf("交易方向必须是 buy 或 sell")
	}
	if transaction.Shares <= 0 || math.IsNaN(transaction.Shares) || math.IsInf(transaction.Shares, 0) || math.Trunc(transaction.Shares) != transaction.Shares {
		return fmt.Errorf("股数必须是正整数")
	}
	if transaction.Price <= 0 || math.IsNaN(transaction.Price) || math.IsInf(transaction.Price, 0) {
		return fmt.Errorf("成交价格必须是正数")
	}
	return nil
}

// ValidateLedger replays transactions in date and input order. This catches
// overselling at any historical point instead of checking only today's net
// position.
func ValidateLedger(ledger *Ledger) error {
	if ledger == nil {
		return fmt.Errorf("交易流水不能为空")
	}
	type indexedTransaction struct {
		index int
		value Transaction
	}
	ordered := make([]indexedTransaction, len(ledger.Transactions))
	for i, transaction := range ledger.Transactions {
		transaction.Code = strings.ToUpper(strings.TrimSpace(transaction.Code))
		transaction.Action = strings.ToLower(strings.TrimSpace(transaction.Action))
		transaction.Comment = strings.TrimSpace(transaction.Comment)
		if err := ValidateTransaction(transaction); err != nil {
			return fmt.Errorf("第 %d 笔交易无效: %w", i+1, err)
		}
		ordered[i] = indexedTransaction{index: i, value: transaction}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].value.Date < ordered[j].value.Date
	})
	holdings := make(map[string]float64)
	for _, item := range ordered {
		transaction := item.value
		if transaction.Action == "buy" {
			holdings[transaction.Code] += transaction.Shares
			continue
		}
		if transaction.Shares > holdings[transaction.Code] {
			return fmt.Errorf("第 %d 笔交易无效: %s 在 %s 卖出 %.0f 股，但当时仅持有 %.0f 股",
				item.index+1, transaction.Code, transaction.Date, transaction.Shares, holdings[transaction.Code])
		}
		holdings[transaction.Code] -= transaction.Shares
	}
	return nil
}
