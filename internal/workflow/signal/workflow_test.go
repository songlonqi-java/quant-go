package signalworkflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quant/internal/dataset"
)

func TestLoadPortfolioSummaryIgnoresMissingPortfolioFile(t *testing.T) {
	summary, err := loadPortfolioSummary(filepath.Join(t.TempDir(), "missing.yaml"), &dataset.Dataset{})

	if err != nil {
		t.Fatalf("loadPortfolioSummary() error = %v, want nil", err)
	}
	if summary != nil {
		t.Fatalf("summary = %+v, want nil", summary)
	}
}

func TestLoadPortfolioSummaryReturnsMalformedPortfolioError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portfolio.yaml")
	if err := os.WriteFile(path, []byte("transactions: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadPortfolioSummary(path, &dataset.Dataset{})

	if err == nil {
		t.Fatal("loadPortfolioSummary() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "加载组合失败") {
		t.Fatalf("error = %v, want 加载组合失败", err)
	}
}
