package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quant/internal/ai"
)

type AIAnswer struct {
	ID               int64
	ReportID         int64
	Question         string
	Answer           string
	Model            string
	CreatedAt        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func (s *taskStore) saveAIAnswer(ctx context.Context, reportID int64, question, answer, model string, completion *ai.Completion) error {
	if completion == nil {
		completion = &ai.Completion{}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO web_ai_answers(report_id, question, answer, model, created_at, prompt_tokens, completion_tokens, total_tokens)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, reportID, question, answer, model, timestamp(),
		completion.PromptTokens, completion.CompletionTokens, completion.TotalTokens)
	return err
}

func (s *taskStore) aiAnswers(ctx context.Context, reportID int64) ([]AIAnswer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, report_id, question, answer, model, created_at, prompt_tokens, completion_tokens, total_tokens
		FROM web_ai_answers WHERE report_id = ? ORDER BY id DESC`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var answers []AIAnswer
	for rows.Next() {
		var answer AIAnswer
		if err := rows.Scan(&answer.ID, &answer.ReportID, &answer.Question, &answer.Answer, &answer.Model, &answer.CreatedAt,
			&answer.PromptTokens, &answer.CompletionTokens, &answer.TotalTokens); err != nil {
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
		"report_version": record.ReportVersion, "code_version": record.CodeVersion,
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

func validateAIAnswer(answer string) error {
	for _, heading := range []string{"报告原始数据", "模型推断", "数据不足", "风险提示"} {
		if !strings.Contains(answer, heading) {
			return fmt.Errorf("AI 回答缺少必需章节：%s", heading)
		}
	}
	return nil
}
