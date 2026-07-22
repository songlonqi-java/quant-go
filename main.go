package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	_ "modernc.org/sqlite" // 纯 Go 实现的 SQLite 驱动
)

// ==================== 1. 配置区域 ====================
const (
	TushareToken = "YOUR_TUSHARE_TOKEN_HERE" // 替换成你的 Token
	StartYear    = 2020                      // 从哪一年开始拉取
	EndYear      = 2026                      // 到哪一年结束（包含）
)

// ==================== 2. 数据结构定义 ====================

// 用于 Tushare 请求的通用结构
type TushareReq struct {
	APIName string                 `json:"api_name"`
	Token   string                 `json:"token"`
	Params  map[string]interface{} `json:"params"`
	Fields  string                 `json:"fields"`
}

// Tushare 响应结构
type TushareResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

// 股票基础信息（对应 stock_basic 接口）
type StockBasic struct {
	TsCode     string `parquet:"ts_code"`
	Symbol     string `parquet:"symbol"`
	Name       string `parquet:"name"`
	Market     string `parquet:"market"`
	ListDate   string `parquet:"list_date"`
	DelistDate string `parquet:"delist_date"`
}

// 日线数据（对应 pro_bar 接口，前复权）
type DailyBar struct {
	TsCode    string  `parquet:"ts_code"`
	TradeDate string  `parquet:"trade_date"`
	Open      float64 `parquet:"open"`
	High      float64 `parquet:"high"`
	Low       float64 `parquet:"low"`
	Close     float64 `parquet:"close"`
	Vol       float64 `parquet:"vol"`    // 成交量（手）
	Amount    float64 `parquet:"amount"` // 成交额（千元）
}

// ==================== 3. 核心工具函数 ====================

// callTushare 通用调用函数，自动处理频率限制（每分钟不超过 200 次，我们控制在每分钟 150 次左右）
func callTushare(apiName string, params map[string]interface{}, fields string) ([]string, [][]interface{}, error) {
	reqBody := TushareReq{
		APIName: apiName,
		Token:   TushareToken,
		Params:  params,
		Fields:  fields,
	}
	jsonData, _ := json.Marshal(reqBody)

	// 频率限制：每次调用间隔 400 毫秒（即每分钟 150 次，留出安全余量）
	time.Sleep(400 * time.Millisecond)

	resp, err := http.Post("http://api.tushare.pro", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result TushareResp
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, fmt.Errorf("JSON解析失败: %v, 原始返回: %s", err, string(body))
	}

	if result.Code != 0 {
		return nil, nil, fmt.Errorf("Tushare接口报错: [%d] %s", result.Code, result.Msg)
	}

	return result.Data.Fields, result.Data.Items, nil
}

// ==================== 4. 获取并过滤股票列表 ====================

func fetchStockList() ([]StockBasic, error) {
	fmt.Println(">>> 开始获取股票基础列表...")
	fields := "ts_code,symbol,name,market,list_date,delist_date"
	params := map[string]interface{}{
		"list_status": "L", // 仅上市状态
		"exchange":    "",  // 全部交易所
	}
	fieldsList, items, err := callTushare("stock_basic", params, fields)
	if err != nil {
		return nil, err
	}

	// 构建字段索引映射
	idxMap := make(map[string]int)
	for i, f := range fieldsList {
		idxMap[f] = i
	}

	var allStocks []StockBasic
	for _, item := range items {
		tsCode := getString(item, idxMap["ts_code"])
		// ---- 过滤逻辑：剔除创业板(30)和科创板(68) ----
		if strings.HasPrefix(tsCode, "30") || strings.HasPrefix(tsCode, "68") {
			continue
		}
		// 额外安全过滤：只保留沪市(60)和深市主板(00,001)
		if !strings.HasPrefix(tsCode, "60") && !strings.HasPrefix(tsCode, "00") && !strings.HasPrefix(tsCode, "001") {
			continue
		}

		allStocks = append(allStocks, StockBasic{
			TsCode:     tsCode,
			Symbol:     getString(item, idxMap["symbol"]),
			Name:       getString(item, idxMap["name"]),
			Market:     getString(item, idxMap["market"]),
			ListDate:   getString(item, idxMap["list_date"]),
			DelistDate: getString(item, idxMap["delist_date"]),
		})
	}

	fmt.Printf(">>> 过滤后共获取 %d 只主板股票\n", len(allStocks))
	return allStocks, nil
}

// ==================== 5. 按年拉取日线数据（前复权） ====================

func fetchDailyByYear(year int, stockCodes []string) error {
	fmt.Printf(">>> 开始拉取 %d 年的日线数据...\n", year)

	// 构建日期范围：该年的 01-01 到 12-31
	startDate := fmt.Sprintf("%d0101", year)
	endDate := fmt.Sprintf("%d1231", year)

	// Tushare pro_bar 接口参数：我们按股票代码循环拉取，避免返回数据过大被截断。
	// 注意：pro_bar 每次只能查一只股票，所以需要循环。
	// 但每天有 10 万次额度，5000 只股票 * 1 年 = 5000 次请求，完全在额度内。
	fields := "ts_code,trade_date,open,high,low,close,vol,amount"

	var allBars []DailyBar
	processed := 0
	total := len(stockCodes)

	for _, code := range stockCodes {
		params := map[string]interface{}{
			"ts_code":    code,
			"start_date": startDate,
			"end_date":   endDate,
			"adj":        "qfq", // 前复权，回测必须！
		}
		fieldList, items, err := callTushare("pro_bar", params, fields)
		if err != nil {
			// 如果某只股票拉取失败（比如刚退市），打印警告并跳过
			fmt.Printf("  警告: 拉取 %s 失败: %v, 跳过\n", code, err)
			continue
		}
		if len(items) == 0 {
			// 该年无交易数据（可能未上市或已退市）
			processed++
			continue
		}

		idxMap := make(map[string]int)
		for i, f := range fieldList {
			idxMap[f] = i
		}

		for _, item := range items {
			allBars = append(allBars, DailyBar{
				TsCode:    getString(item, idxMap["ts_code"]),
				TradeDate: getString(item, idxMap["trade_date"]),
				Open:      getFloat(item, idxMap["open"]),
				High:      getFloat(item, idxMap["high"]),
				Low:       getFloat(item, idxMap["low"]),
				Close:     getFloat(item, idxMap["close"]),
				Vol:       getFloat(item, idxMap["vol"]),
				Amount:    getFloat(item, idxMap["amount"]),
			})
		}
		processed++
		if processed%100 == 0 {
			fmt.Printf("  进度: %d / %d\n", processed, total)
		}
	}

	if len(allBars) == 0 {
		fmt.Printf(">>> %d 年无任何数据，跳过\n", year)
		return nil
	}

	// ---- 保存为 Parquet 文件 ----
	dir := "./data/raw/daily"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %v", err)
	}
	filename := filepath.Join(dir, fmt.Sprintf("%d.parquet", year))

	// 使用 parquet-go 写入
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建文件失败: %v", err)
	}
	defer file.Close()

	writer := parquet.NewWriter(file)
	// 将切片逐行写入
	for _, bar := range allBars {
		if err := writer.Write(bar); err != nil {
			return fmt.Errorf("写入Parquet失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("关闭Writer失败: %v", err)
	}

	fmt.Printf(">>> %d 年数据拉取完成，共 %d 行，保存至 %s\n", year, len(allBars), filename)
	return nil
}

// ==================== 6. 初始化 SQLite 数据库（为因子计算做准备） ====================

func initSQLite() error {
	dbPath := "./data/meta/quant.db"
	if err := os.MkdirAll("./data/meta", 0755); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// 创建因子表（用于后续存放计算好的 MA、RSI、放量信号等）
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS daily_factors (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts_code TEXT NOT NULL,
		trade_date TEXT NOT NULL,
		open REAL,
		high REAL,
		low REAL,
		close REAL,
		vol REAL,
		ma5 REAL,
		ma20 REAL,
		rsi14 REAL,
		volume_ratio REAL,   -- 量比
		signal TEXT,          -- 买入/卖出信号
		UNIQUE(ts_code, trade_date)
	);
	CREATE INDEX IF NOT EXISTS idx_code_date ON daily_factors(ts_code, trade_date);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return err
	}

	fmt.Printf(">>> SQLite 数据库初始化成功: %s\n", dbPath)
	return nil
}

// ==================== 7. 辅助类型转换函数 ====================

func getString(item []interface{}, idx int) string {
	if idx < 0 || idx >= len(item) || item[idx] == nil {
		return ""
	}
	if v, ok := item[idx].(string); ok {
		return v
	}
	return fmt.Sprintf("%v", item[idx])
}

func getFloat(item []interface{}, idx int) float64 {
	if idx < 0 || idx >= len(item) || item[idx] == nil {
		return 0.0
	}
	switch v := item[idx].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		// 处理字符串数字（比如 "123.45"）
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
		return 0.0
	default:
		return 0.0
	}
}

// ==================== 8. Main 入口 ====================

func main() {
	fmt.Println("========================================")
	fmt.Println("   Go 低频量化 - 数据拉取模块启动")
	fmt.Println("========================================")

	// 0. 检查 Token
	if TushareToken == "YOUR_TUSHARE_TOKEN_HERE" {
		fmt.Println("❌ 错误: 请先在代码中替换 YOUR_TUSHARE_TOKEN_HERE 为你的 Tushare Token！")
		os.Exit(1)
	}

	// 1. 初始化 SQLite
	if err := initSQLite(); err != nil {
		fmt.Printf("❌ 初始化数据库失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 获取股票列表
	stocks, err := fetchStockList()
	if err != nil {
		fmt.Printf("❌ 获取股票列表失败: %v\n", err)
		os.Exit(1)
	}
	if len(stocks) == 0 {
		fmt.Println("❌ 没有获取到任何股票，请检查 Tushare Token 或网络")
		os.Exit(1)
	}

	// 将股票代码提取为切片
	var codes []string
	for _, s := range stocks {
		codes = append(codes, s.TsCode)
	}

	// 3. 按年拉取日线数据
	for year := StartYear; year <= EndYear; year++ {
		if err := fetchDailyByYear(year, codes); err != nil {
			fmt.Printf("❌ 拉取 %d 年数据失败: %v\n", year, err)
			// 不退出，继续下一年
			continue
		}
	}

	// 4. 保存股票列表到 Parquet（作为元数据）
	fmt.Println(">>> 保存股票基础列表...")
	stockDir := "./data/raw"
	if err := os.MkdirAll(stockDir, 0755); err == nil {
		stockFile, _ := os.Create(filepath.Join(stockDir, "stocks.parquet"))
		writer := parquet.NewWriter(stockFile)
		for _, s := range stocks {
			writer.Write(s)
		}
		writer.Close()
		stockFile.Close()
		fmt.Println(">>> 股票列表已保存至 ./data/raw/stocks.parquet")
	}

	fmt.Println("========================================")
	fmt.Println("✅ 所有数据拉取任务执行完毕！")
	fmt.Println("   原始数据存放在: ./data/raw/daily/")
	fmt.Println("   元数据库存放在: ./data/meta/quant.db")
	fmt.Println("========================================")
}
