package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type AIAnswer struct {
	ID        int64
	ReportID  int64
	Question  string
	Answer    string
	Model     string
	CreatedAt string
}

func (s *taskStore) saveAIAnswer(ctx context.Context, reportID int64, question, answer, model string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO web_ai_answers(report_id, question, answer, model, created_at) VALUES(?, ?, ?, ?, ?)`,
		reportID, question, answer, model, timestamp())
	return err
}

func (s *taskStore) aiAnswers(ctx context.Context, reportID int64) ([]AIAnswer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, report_id, question, answer, model, created_at
		FROM web_ai_answers WHERE report_id = ? ORDER BY id DESC`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var answers []AIAnswer
	for rows.Next() {
		var answer AIAnswer
		if err := rows.Scan(&answer.ID, &answer.ReportID, &answer.Question, &answer.Answer, &answer.Model, &answer.CreatedAt); err != nil {
			return nil, err
		}
		answers = append(answers, answer)
	}
	return answers, rows.Err()
}

func compactReportContext(record *ReportRecord) (string, error) {
	if record == nil || record.Report == nil {
		return "", fmt.Errorf("报告内容为空")
	}
	report := record.Report
	contextValue := map[string]any{
		"report_id": record.ID, "kind": record.Kind, "trade_date": record.TradeDate,
		"data_version": record.DataVersion, "strategy_version": record.StrategyVersion,
		"position": report.Position, "recommendations": report.Recommendations,
		"watchlist": report.Watchlist, "warnings": report.Warnings,
		"value_monthly": report.ValueMonthly, "value_quarterly": report.ValueQuarterly,
	}
	encoded, err := json.Marshal(contextValue)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func validateAIQuestion(question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("问题不能为空")
	}
	if len([]rune(question)) > 1000 {
		return "", fmt.Errorf("问题不能超过 1000 个字符")
	}
	return question, nil
}
