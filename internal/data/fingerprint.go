package data

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// Fingerprint returns a deterministic digest of all loaded moneyflow rows.
func (s *MoneyflowStore) Fingerprint() string {
	if s == nil {
		return "none"
	}
	keys := make([]string, 0, len(s.flows))
	for key := range s.flows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		writeFingerprintValue(h, key)
		writeFingerprintValue(h, s.flows[key])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Fingerprint returns a deterministic digest of every fundamental input that
// can affect a strategy decision. Data is traversed under the read lock so live
// signal validation cannot race a concurrent store update.
func (fs *FundamentalStore) Fingerprint() string {
	if fs == nil {
		return "none"
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	h := sha256.New()
	writeFingerprintValue(h, fs.loaded)

	dailyCodes := sortedFingerprintKeys(fs.dailyBasics)
	for _, code := range dailyCodes {
		writeFingerprintValue(h, code)
		byDate := fs.dailyBasics[code]
		dates := sortedFingerprintKeys(byDate)
		for _, date := range dates {
			writeFingerprintValue(h, date)
			if row := byDate[date]; row != nil {
				writeFingerprintValue(h, *row)
			}
		}
	}

	for _, code := range sortedFingerprintKeys(fs.finaIndicators) {
		writeFingerprintValue(h, code)
		rows := append([]FinaIndicator(nil), fs.finaIndicators[code]...)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].AnnDate == rows[j].AnnDate {
				return rows[i].EndDate < rows[j].EndDate
			}
			return rows[i].AnnDate < rows[j].AnnDate
		})
		for _, row := range rows {
			writeFingerprintValue(h, row)
		}
	}

	for _, code := range sortedFingerprintKeys(fs.hs300Entries) {
		writeFingerprintValue(h, code)
		rows := append([]HsConst(nil), fs.hs300Entries[code]...)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].InDate == rows[j].InDate {
				return rows[i].OutDate < rows[j].OutDate
			}
			return rows[i].InDate < rows[j].InDate
		})
		for _, row := range rows {
			writeFingerprintValue(h, row)
		}
	}
	for _, code := range sortedFingerprintKeys(fs.hs300Set) {
		if fs.hs300Set[code] {
			writeFingerprintValue(h, code)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type fingerprintWriter interface {
	Write([]byte) (int, error)
}

func writeFingerprintValue(h fingerprintWriter, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte("error:" + err.Error())
	}
	_, _ = h.Write(payload)
	_, _ = h.Write([]byte{0})
}

func sortedFingerprintKeys[V any](source map[string]V) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
