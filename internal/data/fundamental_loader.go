package data

import (
	"fmt"
	"os"
	"path/filepath"
)

func (f *Fetcher) LoadDailyBasicStore() (*FundamentalStore, error) {
	store := NewFundamentalStore()
	dir := filepath.Join(f.rawDir, "daily_basic")
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil || len(files) == 0 {
		return store, nil
	}
	fmt.Printf(">>> 加载基本面数据... (%d 文件)\n", len(files))
	for _, file := range files {
		basics, err := readDailyBasicParquet(file)
		if err != nil {
			fmt.Printf("  警告: 读取 %s 失败: %v\n", file, err)
			continue
		}
		store.LoadDailyBasics(basics)
	}
	return store, nil
}

func (f *Fetcher) LoadFinaStore() (*FundamentalStore, error) {
	store := NewFundamentalStore()

	finaFile := filepath.Join(f.rawDir, "fina", "fina_indicator.parquet")
	if _, err := os.Stat(finaFile); err == nil {
		indicators, err := readFinaParquet(finaFile)
		if err != nil {
			fmt.Printf("  警告: 读取财务指标失败: %v\n", err)
		} else {
			store.LoadFinaIndicators(indicators)
			fmt.Printf(">>> 财务指标已加载: %d 条\n", len(indicators))
		}
	}

	hsFile := filepath.Join(f.rawDir, "index", "hs300.parquet")
	if _, err := os.Stat(hsFile); err == nil {
		consts, err := readHsConstParquet(hsFile)
		if err != nil {
			fmt.Printf("  警告: 读取沪深300失败: %v\n", err)
		} else {
			store.LoadHsConst(consts)
			fmt.Printf(">>> 沪深300成分股已加载: %d 只\n", len(consts))
		}
	}

	return store, nil
}

func writeDailyBasicParquet(path string, basics []DailyBasic) error {
	return writeGenericParquet(path, basics)
}

func writeFinaParquet(path string, data []FinaIndicator) error {
	return writeGenericParquet(path, data)
}

func writeIncomeParquet(path string, data []Income) error {
	return writeGenericParquet(path, data)
}

func writeHsConstParquet(path string, data []HsConst) error {
	return writeGenericParquet(path, data)
}

func readDailyBasicParquet(filePath string) ([]DailyBasic, error) {
	return readGenericParquet[DailyBasic](filePath)
}

func readFinaParquet(filePath string) ([]FinaIndicator, error) {
	return readGenericParquet[FinaIndicator](filePath)
}

func readHsConstParquet(filePath string) ([]HsConst, error) {
	return readGenericParquet[HsConst](filePath)
}
