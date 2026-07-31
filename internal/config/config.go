package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Tushare    TushareConfig    `yaml:"tushare"`
	Data       DataConfig       `yaml:"data"`
	Fetch      FetchConfig      `yaml:"fetch"`
	Backtest   BacktestConfig   `yaml:"backtest"`
	Signal     SignalConfig     `yaml:"signal"`
	Validation ValidationConfig `yaml:"validation"`
}

type TushareConfig struct {
	Token          string `yaml:"token"`
	BaseURL        string `yaml:"base_url"`
	RateLimitMs    int    `yaml:"rate_limit_ms"`
	DailyCallLimit int    `yaml:"daily_call_limit"`
}

type DataConfig struct {
	RawDir  string `yaml:"raw_dir"`
	MetaDir string `yaml:"meta_dir"`
	DBPath  string `yaml:"db_path"`
}

type FetchConfig struct {
	StartYear     int      `yaml:"start_year"`
	EndYear       int      `yaml:"end_year"`
	StockPrefixes []string `yaml:"stock_prefixes"`
	MinMarketCap  float64  `yaml:"min_market_cap"`
}

type BacktestConfig struct {
	InitialCapital float64 `yaml:"initial_capital"`
	Commission     float64 `yaml:"commission"`
	Slippage       float64 `yaml:"slippage"`
	RiskFreeRate   float64 `yaml:"risk_free_rate"`
	LotSize        float64 `yaml:"lot_size"`
}

type SignalConfig struct {
	DefaultStrategies []string `yaml:"default_strategies"`
	TopN              int      `yaml:"top_n"`
}

// ValidationConfig controls the out-of-sample evidence required before a
// historical signal may be promoted into the formal recommendation list.
// Path is relative to data.raw_dir when it is not absolute.
type ValidationConfig struct {
	Enabled           bool    `yaml:"enabled"`
	Path              string  `yaml:"path"`
	MinSamples        int     `yaml:"min_samples"`
	MinPositiveFolds  int     `yaml:"min_positive_folds"`
	MinExpectedReturn float64 `yaml:"min_expected_return_pct"`
	PriorSamples      float64 `yaml:"prior_samples"`
}

var defaultConfig = Config{
	Tushare: TushareConfig{
		BaseURL:        "http://api.tushare.pro",
		RateLimitMs:    350,
		DailyCallLimit: 5000,
	},
	Data: DataConfig{
		RawDir:  "./data/raw",
		MetaDir: "./data/meta",
		DBPath:  "./data/meta/quant.db",
	},
	Fetch: FetchConfig{
		StartYear:     2020,
		EndYear:       2026,
		StockPrefixes: []string{"60", "00", "001"},
		MinMarketCap:  0,
	},
	Backtest: BacktestConfig{
		InitialCapital: 100000.0,
		Commission:     0.0003,
		Slippage:       0.0001,
		RiskFreeRate:   0.03,
		LotSize:        100,
	},
	Signal: SignalConfig{
		DefaultStrategies: []string{
			"ma_crossover", "macd", "rsi", "bollinger",
			"volume_breakout", "value_ma60", "etf_rotation",
			"dividend_deviation", "bull_flag",
			"kdj", "williams_r", "donchian", "mfi",
			"sar", "roc", "ma_sticky", "limit_up",
			"bottom_reversal", "relative_strength", "atr_breakout",
			"trend_pullback", "quality_value", "earnings_growth",
		},
		TopN: 20,
	},
	Validation: ValidationConfig{
		Enabled:           true,
		Path:              "validation/evidence.json",
		MinSamples:        30,
		MinPositiveFolds:  2,
		MinExpectedReturn: 0,
		PriorSamples:      20,
	},
}

func Load(configPath string) (*Config, error) {
	cfg := defaultConfig

	if configPath == "" {
		configPath = "config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, applyEnvOverrides(&cfg)
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("QUANT_TUSHARE_TOKEN"); v != "" {
		cfg.Tushare.Token = v
	}
	if v := os.Getenv("QUANT_BASE_URL"); v != "" {
		cfg.Tushare.BaseURL = v
	}
	if v := os.Getenv("QUANT_RATE_LIMIT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Tushare.RateLimitMs = n
		}
	}
	if v := os.Getenv("QUANT_DAILY_CALL_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Tushare.DailyCallLimit = n
		}
	}
	if v := os.Getenv("QUANT_DATA_DIR"); v != "" {
		cfg.Data.RawDir = v + "/raw"
		cfg.Data.MetaDir = v + "/meta"
		cfg.Data.DBPath = v + "/meta/quant.db"
	}
	if v := os.Getenv("QUANT_INITIAL_CAPITAL"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Backtest.InitialCapital = f
		}
	}
	if v := os.Getenv("QUANT_RISK_FREE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Backtest.RiskFreeRate = f
		}
	}
	return nil
}

func MustLoad(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		panic(fmt.Sprintf("加载配置失败: %v", err))
	}
	return cfg
}
