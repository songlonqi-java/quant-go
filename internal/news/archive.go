package news

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"quant/internal/data"

	"github.com/parquet-go/parquet-go"
)

const (
	newsArchiveVersion      = "news-archive-v1"
	newsTimeObserved        = "published_and_received"
	newsTimeReceivedOnly    = "received_only"
	newsTimePublishedOnly   = "published_only"
	newsTimeUnknown         = "unknown"
	newsArchiveRelativePath = "news/archive.parquet"
	legacyNewsRelativePath  = "news/latest.parquet"
)

// NewsRecord is an immutable version of one source article. CanonicalID
// groups revisions of the same source URL (or the best available fallback
// identity); ID includes the content hash and identifies one exact version.
type NewsRecord struct {
	ArchiveVersion string `parquet:"archive_version"`
	ID             string `parquet:"id"`
	CanonicalID    string `parquet:"canonical_id"`
	Revision       int    `parquet:"revision"`
	SupersedesID   string `parquet:"supersedes_id"`
	Source         string `parquet:"source"`
	URL            string `parquet:"url"`
	Title          string `parquet:"title"`
	Content        string `parquet:"content"`
	PublishedRaw   string `parquet:"published_raw"`
	PublishedAt    string `parquet:"published_at"`
	ReceivedAt     string `parquet:"received_at"`
	FetchedAt      string `parquet:"fetched_at"`
	ContentHash    string `parquet:"content_hash"`
	TimeConfidence string `parquet:"time_confidence"`
}

// legacyNewsItem deliberately mirrors the old latest.parquet schema. Keeping
// it separate lets an upgraded binary migrate old files after NewsItem grows
// new fields.
type legacyNewsItem struct {
	Datetime string `parquet:"datetime"`
	Content  string `parquet:"content"`
	Title    string `parquet:"title"`
	Source   string `parquet:"source"`
}

func LoadArchive(rawDir string) ([]NewsRecord, bool, error) {
	archivePath := filepath.Join(rawDir, newsArchiveRelativePath)
	records, err := readNewsRecords(archivePath)
	if err == nil {
		return records, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	legacyPath := filepath.Join(rawDir, legacyNewsRelativePath)
	legacy, err := readLegacyNews(legacyPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return migrateLegacyNews(legacy), true, nil
}

func mergeNewsRecords(existing []NewsRecord, incoming []data.NewsItem, observedAt time.Time) []NewsRecord {
	records := append([]NewsRecord(nil), existing...)
	byID := make(map[string]bool, len(records)+len(incoming))
	latest := make(map[string]NewsRecord)
	for _, record := range records {
		if record.ID == "" || record.CanonicalID == "" {
			continue
		}
		byID[record.ID] = true
		current, ok := latest[record.CanonicalID]
		if !ok || record.Revision > current.Revision {
			latest[record.CanonicalID] = record
		}
	}

	candidates := make([]NewsRecord, 0, len(incoming))
	for _, item := range incoming {
		candidate := newNewsRecord(item, observedAt, true)
		if candidate.ID != "" && !byID[candidate.ID] {
			candidates = append(candidates, candidate)
			byID[candidate.ID] = true
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CanonicalID != candidates[j].CanonicalID {
			return candidates[i].CanonicalID < candidates[j].CanonicalID
		}
		if candidates[i].PublishedAt != candidates[j].PublishedAt {
			return candidates[i].PublishedAt < candidates[j].PublishedAt
		}
		return candidates[i].ID < candidates[j].ID
	})
	for _, candidate := range candidates {
		if previous, ok := latest[candidate.CanonicalID]; ok {
			candidate.Revision = previous.Revision + 1
			candidate.SupersedesID = previous.ID
		}
		records = append(records, candidate)
		latest[candidate.CanonicalID] = candidate
	}
	sortNewsRecords(records)
	return records
}

func migrateLegacyNews(items []legacyNewsItem) []NewsRecord {
	result := make([]NewsRecord, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		record := newNewsRecord(data.NewsItem{
			Datetime: item.Datetime, Content: item.Content, Title: item.Title, Source: item.Source,
		}, time.Time{}, false)
		if record.ID == "" || seen[record.ID] {
			continue
		}
		seen[record.ID] = true
		result = append(result, record)
	}
	sortNewsRecords(result)
	return result
}

func newNewsRecord(item data.NewsItem, observedAt time.Time, observed bool) NewsRecord {
	publishedAt, hasPublishedTime := normalizeNewsTime(item.Datetime)
	title := strings.TrimSpace(item.Title)
	content := strings.TrimSpace(item.Content)
	if title == "" && content == "" {
		return NewsRecord{}
	}
	source := strings.TrimSpace(item.Source)
	url := strings.TrimSpace(item.URL)
	contentHash := hashNewsValue(normalizeNewsText(title) + "\n" + normalizeNewsText(content))
	canonicalKey := source + "\n" + url
	if url == "" {
		canonicalKey = source + "\n" + publishedAt + "\n" + normalizeNewsText(title)
	}
	canonicalID := hashNewsValue(canonicalKey)
	record := NewsRecord{
		ArchiveVersion: newsArchiveVersion,
		CanonicalID:    canonicalID,
		Revision:       1,
		Source:         source, URL: url, Title: title, Content: content,
		PublishedRaw: strings.TrimSpace(item.Datetime), PublishedAt: publishedAt,
		ContentHash: contentHash,
	}
	record.ID = hashNewsValue(record.CanonicalID + "\n" + record.ContentHash)
	if observed && !observedAt.IsZero() {
		observedAt = observedAt.UTC()
		record.ReceivedAt = observedAt.Format(time.RFC3339Nano)
		record.FetchedAt = record.ReceivedAt
		if hasPublishedTime {
			record.TimeConfidence = newsTimeObserved
		} else {
			record.TimeConfidence = newsTimeReceivedOnly
		}
	} else if hasPublishedTime {
		record.TimeConfidence = newsTimePublishedOnly
	} else {
		record.TimeConfidence = newsTimeUnknown
	}
	return record
}

func latestNewsItems(records []NewsRecord) []data.NewsItem {
	latest := make(map[string]NewsRecord)
	for _, record := range records {
		current, ok := latest[record.CanonicalID]
		if !ok || record.Revision > current.Revision {
			latest[record.CanonicalID] = record
		}
	}
	selected := make([]NewsRecord, 0, len(latest))
	for _, record := range latest {
		selected = append(selected, record)
	}
	sortNewsRecords(selected)
	items := make([]data.NewsItem, 0, len(selected))
	for _, record := range selected {
		datetime := record.PublishedRaw
		if datetime == "" {
			datetime = record.PublishedAt
		}
		items = append(items, data.NewsItem{
			Datetime: datetime, Content: record.Content, Title: record.Title, Source: record.Source, URL: record.URL,
		})
	}
	return items
}

func saveNewsArchive(rawDir string, records []NewsRecord) error {
	path := filepath.Join(rawDir, newsArchiveRelativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建新闻归档目录: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "archive-*.parquet.tmp")
	if err != nil {
		return fmt.Errorf("创建新闻归档临时文件: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	writer := parquet.NewGenericWriter[NewsRecord](tmp)
	if _, err := writer.Write(records); err != nil {
		cleanup()
		return fmt.Errorf("写入新闻归档: %w", err)
	}
	if err := writer.Close(); err != nil {
		cleanup()
		return fmt.Errorf("关闭新闻归档 writer: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("同步新闻归档: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("关闭新闻归档: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换新闻归档: %w", err)
	}
	return nil
}

func readNewsRecords(path string) ([]NewsRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := parquet.NewGenericReader[NewsRecord](file)
	defer reader.Close()
	return readParquetRows(reader)
}

func readLegacyNews(path string) ([]legacyNewsItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := parquet.NewGenericReader[legacyNewsItem](file)
	defer reader.Close()
	return readParquetRows(reader)
}

type parquetRowReader[T any] interface {
	Read([]T) (int, error)
}

func readParquetRows[T any](reader parquetRowReader[T]) ([]T, error) {
	rows := make([]T, 0)
	buffer := make([]T, 128)
	for {
		count, err := reader.Read(buffer)
		rows = append(rows, buffer[:count]...)
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func sortNewsRecords(records []NewsRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].PublishedAt != records[j].PublishedAt {
			return records[i].PublishedAt > records[j].PublishedAt
		}
		if records[i].CanonicalID != records[j].CanonicalID {
			return records[i].CanonicalID < records[j].CanonicalID
		}
		return records[i].Revision > records[j].Revision
	})
}

func normalizeNewsTime(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	formats := []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05", "20060102150405", "20060102 15:04:05", "20060102",
	}
	for _, format := range formats {
		var parsed time.Time
		var err error
		if format == time.RFC3339Nano || format == time.RFC3339 {
			parsed, err = time.Parse(format, value)
		} else {
			parsed, err = time.ParseInLocation(format, value, location)
		}
		if err == nil {
			return parsed.Format(time.RFC3339), true
		}
	}
	return value, false
}

func normalizeNewsText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func hashNewsValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
