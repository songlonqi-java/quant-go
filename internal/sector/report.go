package sector

import (
	"fmt"
	"sort"
	"strings"

	"quant/internal/data"
)

type Report struct {
	TradeDate string
	Sectors   []data.SectorDaily
	byKey     map[string]data.SectorDaily
}

func NewReport(rows []data.SectorDaily) *Report {
	r := &Report{byKey: make(map[string]data.SectorDaily, len(rows))}
	for _, row := range rows {
		if r.TradeDate == "" || row.TradeDate > r.TradeDate {
			r.TradeDate = row.TradeDate
		}
		r.Sectors = append(r.Sectors, row)
		r.byKey[reportKey(row.SectorType, row.SectorCode)] = row
	}
	sort.Slice(r.Sectors, func(i, j int) bool {
		if r.Sectors[i].Chg1 == r.Sectors[j].Chg1 {
			return r.Sectors[i].SectorName < r.Sectors[j].SectorName
		}
		return r.Sectors[i].Chg1 > r.Sectors[j].Chg1
	})
	return r
}

func (r *Report) Find(sectorType, sectorCode string) (data.SectorDaily, bool) {
	if r == nil {
		return data.SectorDaily{}, false
	}
	row, ok := r.byKey[reportKey(sectorType, sectorCode)]
	return row, ok
}

func (r *Report) Print() {
	if r == nil || len(r.Sectors) == 0 {
		return
	}
	fmt.Println("\n========== 板块异动 ==========")
	fmt.Printf("统计日期: %s, 板块数: %d\n", r.TradeDate, len(r.Sectors))
	printSectorLine("强势板块", topSectors(r.Sectors, func(row data.SectorDaily) bool { return row.Chg1 > 0 }, 5))
	printSectorLine("资金确认", topSectors(r.Sectors, func(row data.SectorDaily) bool { return HasTag(row, "资金确认") }, 5))
	printSectorLine("风险板块", topSectors(r.Sectors, func(row data.SectorDaily) bool {
		return HasTag(row, "资金背离") || HasTag(row, "高位退潮") || HasTag(row, "孤立龙头")
	}, 5))
	fmt.Println("==============================")
}

func topSectors(rows []data.SectorDaily, keep func(data.SectorDaily) bool, limit int) []data.SectorDaily {
	var out []data.SectorDaily
	for _, row := range rows {
		if keep(row) {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chg1 == out[j].Chg1 {
			return out[i].SectorName < out[j].SectorName
		}
		return out[i].Chg1 > out[j].Chg1
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func printSectorLine(label string, rows []data.SectorDaily) {
	if len(rows) == 0 {
		fmt.Printf("%s: 无\n", label)
		return
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		tags := SplitTags(row.Tags)
		tagText := ""
		if len(tags) > 0 {
			tagText = "/" + strings.Join(tags, ",")
		}
		parts = append(parts, fmt.Sprintf("%s(%+.1f%%, %.0f%%)%s", row.SectorName, row.Chg1, row.Breadth, tagText))
	}
	fmt.Printf("%s: %s\n", label, strings.Join(parts, " / "))
}

func reportKey(sectorType, sectorCode string) string {
	return sectorType + "|" + sectorCode
}
