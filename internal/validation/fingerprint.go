package validation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"

	"quant/internal/data"
	"quant/internal/strategy"
)

// decisionModelVersion is bumped whenever aggregation, qualification, or
// execution semantics change without changing a strategy's public parameters.
const decisionModelVersion = 2

type strategyFingerprintEntry struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

func StrategyFingerprint(strategies []strategy.Strategy, commission, slippage float64) (string, error) {
	entries := make([]strategyFingerprintEntry, 0, len(strategies))
	for _, current := range strategies {
		if current == nil {
			continue
		}
		config, err := json.Marshal(current)
		if err != nil {
			return "", fmt.Errorf("序列化策略 %s 参数: %w", current.Name(), err)
		}
		entries = append(entries, strategyFingerprintEntry{
			Name:   current.Name(),
			Type:   reflect.TypeOf(current).String(),
			Config: config,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == entries[j].Name {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].Name < entries[j].Name
	})
	payload := struct {
		DecisionModelVersion int                        `json:"decision_model_version"`
		Commission           float64                    `json:"commission"`
		Slippage             float64                    `json:"slippage"`
		Strategies           []strategyFingerprintEntry `json:"strategies"`
	}{decisionModelVersion, commission, slippage, entries}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (s *Store) ValidateCompatibility(strategies []strategy.Strategy, commission, slippage float64, codeMap map[string][]data.DailyBar, fundamentals *data.FundamentalStore, moneyflows *data.MoneyflowStore) error {
	if s == nil {
		return fmt.Errorf("历史验证证据为空")
	}
	expected, err := StrategyFingerprint(strategies, commission, slippage)
	if err != nil {
		return err
	}
	if s.StrategyFingerprint == "" || s.DataFingerprint == "" || s.BuildFingerprint == "" {
		return fmt.Errorf("历史验证证据缺少构建指纹，需要重新执行 validate build")
	}
	if s.StrategyFingerprint != expected {
		return fmt.Errorf("历史验证证据与当前策略、费用或决策模型不兼容，需要重新执行 validate build")
	}
	expectedData := fingerprintData(codeMap, fundamentals, moneyflows)
	if s.DataFingerprint != expectedData {
		return fmt.Errorf("历史验证证据与当前行情数据不兼容，需要重新执行 validate build")
	}
	expectedBuild := fingerprintBuild(s.StrategyFingerprint, s.DataFingerprint, s.StartDate, s.EndDate, len(s.Folds))
	if s.BuildFingerprint != expectedBuild {
		return fmt.Errorf("历史验证证据的构建指纹无效，需要重新执行 validate build")
	}
	latestDate := latestFingerprintDate(codeMap)
	if latestDate != "" && (s.EndDate == "" || s.EndDate < latestDate) {
		return fmt.Errorf("历史验证证据只到 %s，落后于行情 %s，需要重新执行 validate build", s.EndDate, latestDate)
	}
	return nil
}

func latestFingerprintDate(codeMap map[string][]data.DailyBar) string {
	latest := ""
	for _, bars := range codeMap {
		for _, bar := range bars {
			if bar.TradeDate > latest {
				latest = bar.TradeDate
			}
		}
	}
	return latest
}

func fingerprintData(codeMap map[string][]data.DailyBar, fundamentals *data.FundamentalStore, moneyflows *data.MoneyflowStore) string {
	h := sha256.New()
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		writeHashString(h, code)
		for _, bar := range codeMap[code] {
			writeHashString(h, bar.TradeDate)
			for _, value := range []float64{
				bar.Open, bar.High, bar.Low, bar.Close, bar.Vol, bar.Amount,
				bar.RawOpen, bar.RawHigh, bar.RawLow, bar.RawClose, bar.AdjFactor,
				bar.UpLimit, bar.DownLimit,
			} {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], math.Float64bits(value))
				_, _ = h.Write(buf[:])
			}
		}
	}
	writeHashString(h, "fundamentals")
	writeHashString(h, fundamentals.Fingerprint())
	writeHashString(h, "moneyflows")
	writeHashString(h, moneyflows.Fingerprint())
	return fmt.Sprintf("%x", h.Sum(nil))
}

func fingerprintBuild(strategyFingerprint, dataFingerprint, startDate, endDate string, folds int) string {
	payload := fmt.Sprintf("%d|%d|%s|%s|%s|%s|%d", formatVersion, decisionModelVersion, strategyFingerprint, dataFingerprint, startDate, endDate, folds)
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum[:])
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func writeHashString(h hashWriter, value string) {
	var size [8]byte
	binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(value))
}
