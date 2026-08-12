package news

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

func TestMergeNewsRecordsDeduplicatesAndKeepsRevisions(t *testing.T) {
	firstSeen := time.Date(2026, 8, 1, 1, 2, 3, 456, time.UTC)
	item := data.NewsItem{
		Datetime: "2026-08-01 08:00:00", Title: "某公司获得重大订单", Content: "订单金额十亿元",
		Source: "测试来源", URL: "https://example.com/news/1",
	}
	records := mergeNewsRecords(nil, []data.NewsItem{item, item}, firstSeen)
	if len(records) != 1 {
		t.Fatalf("deduplicated records = %d, want 1: %+v", len(records), records)
	}
	first := records[0]
	if first.ID == "" || first.CanonicalID == "" || first.ContentHash == "" || first.Revision != 1 {
		t.Fatalf("incomplete first record: %+v", first)
	}
	if first.PublishedAt != "2026-08-01T08:00:00+08:00" || first.ReceivedAt != firstSeen.Format(time.RFC3339Nano) || first.TimeConfidence != newsTimeObserved {
		t.Fatalf("incorrect time audit: %+v", first)
	}

	records = mergeNewsRecords(records, []data.NewsItem{item}, firstSeen.Add(time.Hour))
	if len(records) != 1 || records[0].ReceivedAt != first.ReceivedAt {
		t.Fatalf("repeat fetch changed immutable fact: %+v", records)
	}

	revised := item
	revised.Content = "订单金额调整为十二亿元"
	records = mergeNewsRecords(records, []data.NewsItem{revised}, firstSeen.Add(2*time.Hour))
	if len(records) != 2 {
		t.Fatalf("revision records = %d, want 2: %+v", len(records), records)
	}
	items := latestNewsItems(records)
	if len(items) != 1 || items[0].Content != revised.Content {
		t.Fatalf("latest revision = %+v", items)
	}
	var revision NewsRecord
	for _, record := range records {
		if record.Revision == 2 {
			revision = record
		}
	}
	if revision.ID == "" || revision.SupersedesID != first.ID || revision.CanonicalID != first.CanonicalID {
		t.Fatalf("revision chain = %+v, first = %+v", revision, first)
	}
}

func TestLoadNewsArchiveMigratesLegacyLatestFile(t *testing.T) {
	rawDir := t.TempDir()
	newsDir := filepath.Join(rawDir, "news")
	if err := os.MkdirAll(newsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(newsDir, "latest.parquet")
	file, err := os.Create(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[legacyNewsItem](file)
	legacy := legacyNewsItem{Datetime: "20260801090000", Title: "旧新闻", Content: "旧正文", Source: "旧来源"}
	if _, err := writer.Write([]legacyNewsItem{legacy, legacy}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	records, migrated, err := LoadArchive(rawDir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || len(records) != 1 {
		t.Fatalf("migration = %v, records = %+v", migrated, records)
	}
	if records[0].ReceivedAt != "" || records[0].TimeConfidence != newsTimePublishedOnly {
		t.Fatalf("legacy receive time was fabricated: %+v", records[0])
	}
	if err := saveNewsArchive(rawDir, records); err != nil {
		t.Fatal(err)
	}
	reloaded, migratedAgain, err := LoadArchive(rawDir)
	if err != nil {
		t.Fatal(err)
	}
	if migratedAgain || !reflect.DeepEqual(reloaded, records) {
		t.Fatalf("archive reload = migrated %v\n got: %+v\nwant: %+v", migratedAgain, reloaded, records)
	}
}

func TestNewNewsRecordRejectsEmptyArticle(t *testing.T) {
	record := newNewsRecord(data.NewsItem{Source: "source"}, time.Now(), true)
	if record.ID != "" {
		t.Fatalf("empty article was archived: %+v", record)
	}
}
