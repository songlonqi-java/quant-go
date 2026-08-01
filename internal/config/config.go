package config

import (
	"fmt"
	"os"
	"strconv"

	"quant/internal/execution"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Tushare    TushareConfig    `yaml:"tushare"`
	Data       DataConfig       `yaml:"data"`
	Fetch      FetchConfig      `yaml:"fetch"`
	Backtest   BacktestConfig   `yaml:"backtest"`
	Liquidity  LiquidityConfig  `yaml:"liquidity"`
	Signal     SignalConfig     `yaml:"signal"`
	Portfolio  PortfolioConfig  `yaml:"portfolio"`
	Validation ValidationConfig `yaml:"validation"`
	AI         AIConfig         `yaml:"ai"`
	Backup     BackupConfig     `yaml:"backup"`
}

type AIConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	APIKeyEnv  string `yaml:"api_key_env"`
	Model      string `yaml:"model"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type BackupConfig struct {
	Dir       string `yaml:"dir"`
	Retention int    `yaml:"retention"`
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

type LiquidityConfig struct {
	Enabled             bool    `yaml:"enabled"`
	MinListingDays      int     `yaml:"min_listing_days"`
	AmountLookback      int     `yaml:"amount_lookback"`
	MinAverageAmountCNY float64 `yaml:"min_average_amount_cny"`
	MinTurnoverRatePct  float64 `yaml:"min_turnover_rate_pct"`
	RequireTurnoverData bool    `yaml:"require_turnover_data"`
	MaxParticipationPct float64 `yaml:"max_participation_pct"`
	ImpactCoefficient   float64 `yaml:"impact_coefficient"`
	MaxImpactRate       float64 `yaml:"max_impact_rate"`
}

func (c LiquidityConfig) Policy() execution.LiquidityPolicy {
	return execution.LiquidityPolicy{
		Enabled:             c.Enabled,
		MinListingDays:      c.MinListingDays,
		AmountLookback:      c.AmountLookback,
		MinAverageAmountCNY: c.MinAverageAmountCNY,
		MinTurnoverRatePct:  c.MinTurnoverRatePct,
		RequireTurnoverData: c.RequireTurnoverData,
		MaxParticipationPct: c.MaxParticipationPct,
		ImpactCoefficient:   c.ImpactCoefficient,
		MaxImpactRate:       c.MaxImpactRate,
	}
}

type SignalConfig struct {
	DefaultStrategies []string `yaml:"default_strategies"`
	TopN              int      `yaml:"top_n"`
}

// PortfolioConfig defines account-level risk limits shared by live signal
// allocation and the multi-security portfolio backtest. ReferenceEquity is the
// current account equity used to translate portfolio.yaml market values into
// exposure percentages; it should include both cash and positions.
type PortfolioConfig struct {
	ReferenceEquity      float64 `yaml:"reference_equity"`
	MaxTotalPositionPct  float64 `yaml:"max_total_position_pct"`
	MaxSinglePositionPct float64 `yaml:"max_single_position_pct"`
	MaxSectorPositionPct float64 `yaml:"max_sector_position_pct"`
}

func DefaultPortfolioConfig() PortfolioConfig {
	return PortfolioConfig{
		ReferenceEquity:      100000,
		MaxTotalPositionPct:  70,
		MaxSinglePositionPct: 15,
		MaxSectorPositionPct: 25,
	}
}

// Normalized supplies safe defaults for Config values constructed directly in
// tests or by library callers instead of going through Load.
func (p PortfolioConfig) Normalized(fallbackEquity float64) PortfolioConfig {
	defaults := DefaultPortfolioConfig()
	if p.ReferenceEquity <= 0 {
		p.ReferenceEquity = fallbackEquity
		if p.ReferenceEquity <= 0 {
			p.ReferenceEquity = defaults.ReferenceEquity
		}
	}
	if p.MaxTotalPositionPct <= 0 {
		p.MaxTotalPositionPct = defaults.MaxTotalPositionPct
	}
	if p.MaxSinglePositionPct <= 0 {
		p.MaxSinglePositionPct = defaults.MaxSinglePositionPct
	}
	if p.MaxSectorPositionPct <= 0 {
		p.MaxSectorPositionPct = defaults.MaxSectorPositionPct
	}
	return p
}

// ValidationConfig controls the out-of-sample evidence required before a
// historical signal may be promoted into the formal recommendation list.
// Path is relative to data.raw_dir when it is not absolute. MinSamples counts
// independent, signal-date clusters rather than individual stock trades.
// PriorSamples is the maximum equivalent sample weight assigned to broad
// horizon or regime statistics when shrinking strategy-specific evidence.
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
	Liquidity: LiquidityConfig{
		Enabled:             true,
		MinListingDays:      60,
		AmountLookback:      20,
		MinAverageAmountCNY: 20_000_000,
		MinTurnoverRatePct:  0.5,
		RequireTurnoverData: false,
		MaxParticipationPct: 5,
		ImpactCoefficient:   0.005,
		MaxImpactRate:       0.02,
	},
	Signal: SignalConfig{
		DefaultStrategies: []string{
			"ma_crossover", "macd", "rsi", "bollinger",
			"volume_breakout", "bull_flag",
			"kdj", "williams_r", "donchian", "mfi",
			"sar", "roc", "ma_sticky", "limit_up",
			"relative_strength", "atr_breakout", "trend_pullback",
		},
		TopN: 20,
	},
	Portfolio: DefaultPortfolioConfig(),
	Validation: ValidationConfig{
		Enabled:           true,
		Path:              "validation/evidence.json",
		MinSamples:        30,
		MinPositiveFolds:  2,
		MinExpectedReturn: 0,
		PriorSamples:      20,
	},
	AI: AIConfig{
		BaseURL:    "https://api.deepseek.com",
		APIKeyEnv:  "QUANT_AI_API_KEY",
		Model:      "deepseek-chat",
		TimeoutSec: 60,
	},
	Backup: BackupConfig{Retention: 14},
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
	if v := os.Getenv("QUANT_AI_BASE_URL"); v != "" {
		cfg.AI.BaseURL = v
	}
	if v := os.Getenv("QUANT_AI_MODEL"); v != "" {
		cfg.AI.Model = v
	}
	keyEnv := cfg.AI.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "QUANT_AI_API_KEY"
	}
	if v := os.Getenv(keyEnv); v != "" {
		cfg.AI.APIKey = v
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
