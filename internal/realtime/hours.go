package realtime

import "time"

var chinaLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func ChinaTradeDate(now time.Time) string {
	return now.In(chinaLocation).Format("20060102")
}

// IsAShareTradingHours accepts the two continuous A-share trading sessions.
// It excludes the lunch recess, weekends, and all time after 15:00, when the
// daily workflow should rely on the completed Tushare daily bar instead.
func IsAShareTradingHours(now time.Time) bool {
	local := now.In(chinaLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return false
	}
	minute := local.Hour()*60 + local.Minute()
	return (minute >= 9*60+30 && minute <= 11*60+30) ||
		(minute >= 13*60 && minute <= 15*60)
}
