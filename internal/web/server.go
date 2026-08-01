// Package web exposes the local, single-user Web interface. It binds only to
// loopback by default; authentication and public deployment belong to a later
// phase rather than being implied by this development server.
package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"quant/internal/config"
	"quant/internal/workflow/daily"
)

type Options struct {
	Config        *config.Config
	DatabasePath  string
	PortfolioPath string
}

type Server struct {
	store         *taskStore
	runner        *taskRunner
	mux           *http.ServeMux
	portfolioPath string
}

func New(opts Options) (*Server, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("缺少配置")
	}
	return newServer(opts, nil)
}

func newServer(opts Options, execute taskExecutor) (*Server, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("缺少配置")
	}
	if opts.DatabasePath == "" {
		opts.DatabasePath = filepath.Join(opts.Config.Data.MetaDir, "web.db")
	}
	if opts.PortfolioPath == "" {
		opts.PortfolioPath = "portfolio.yaml"
	}
	store, err := openTaskStore(opts.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("打开任务数据库: %w", err)
	}
	if _, err := store.recoverInterrupted(context.Background()); err != nil {
		store.close()
		return nil, fmt.Errorf("恢复中断任务: %w", err)
	}
	server := &Server{store: store, portfolioPath: opts.PortfolioPath}
	if execute == nil {
		execute = defaultExecutor(opts, store)
	}
	server.runner = newTaskRunner(store, execute)
	server.mux = http.NewServeMux()
	server.mux.HandleFunc("GET /healthz", server.handleHealth)
	server.mux.HandleFunc("POST /tasks/daily", server.handleCreateDaily)
	server.mux.HandleFunc("POST /portfolio/import-yaml", server.handlePortfolioImport)
	server.mux.HandleFunc("GET /portfolio/export-yaml", server.handlePortfolioExport)
	server.mux.HandleFunc("GET /tasks/", server.handleTask)
	server.mux.HandleFunc("GET /", server.handleHome)
	server.runner.start()
	return server, nil
}

func defaultExecutor(opts Options, store *taskStore) taskExecutor {
	return func(ctx context.Context, update func(string)) (*DailyReport, error) {
		ledger, err := store.portfolioLedger(ctx)
		if err != nil {
			return nil, fmt.Errorf("加载 SQLite 交易流水: %w", err)
		}
		result, err := daily.Run(ctx, daily.Options{
			Config:          opts.Config,
			PortfolioLedger: ledger,
			TopN:            10,
			WatchN:          5,
			Progress: func(step daily.Step) {
				update(fmt.Sprintf("%s：%s", step.Name, step.Detail))
			},
		})
		return reportFromDaily(result), err
	}
}

func (s *Server) handlePortfolioImport(w http.ResponseWriter, r *http.Request) {
	if s.portfolioPath == "" {
		http.Error(w, "未配置 YAML 持仓路径", http.StatusBadRequest)
		return
	}
	result, err := s.store.importPortfolioYAML(r.Context(), s.portfolioPath)
	if errors.Is(err, ErrPortfolioAlreadyImported) {
		http.Redirect(w, r, "/?portfolio=already-imported", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/?portfolio=import-error&detail="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/?portfolio=imported&count=%d", result.TransactionCount), http.StatusSeeOther)
}

func (s *Server) handlePortfolioExport(w http.ResponseWriter, r *http.Request) {
	contents, err := s.store.exportPortfolioYAML(r.Context())
	if err != nil {
		http.Error(w, "导出交易流水失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="portfolio.yaml"`)
	_, _ = w.Write(contents)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Close() error {
	if s.runner != nil {
		s.runner.stop()
	}
	if s.store != nil {
		return s.store.close()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.list(r.Context(), 20)
	if err != nil {
		http.Error(w, "读取任务列表失败", http.StatusInternalServerError)
		return
	}
	transactions, portfolioErr := s.store.portfolioTransactions(r.Context(), false)
	data := homePageData{
		Tasks:           tasks,
		Error:           r.URL.Query().Get("error"),
		PortfolioStatus: r.URL.Query().Get("portfolio"),
		PortfolioDetail: r.URL.Query().Get("detail"),
		PortfolioCount:  len(transactions),
	}
	if portfolioErr != nil {
		data.PortfolioStatus = "read-error"
		data.PortfolioDetail = portfolioErr.Error()
	}
	for _, task := range tasks {
		if task.Status == TaskQueued || task.Status == TaskRunning {
			data.HasActive = true
			break
		}
	}
	renderPage(w, homeTemplate, data)
}

func (s *Server) handleCreateDaily(w http.ResponseWriter, r *http.Request) {
	task, err := s.runner.enqueue(r.Context())
	if errors.Is(err, ErrTaskAlreadyActive) {
		http.Redirect(w, r, "/?error=active", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "创建日终任务失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/tasks/%d", task.ID), http.StatusSeeOther)
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	task, err := s.store.task(r.Context(), id, true)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "读取任务失败", http.StatusInternalServerError)
		return
	}
	data := taskPageData{Task: task}
	if task.Report != nil {
		data.CanRecommend = task.Report.Position.Action != "空仓" && task.Report.Position.Action != "观望"
	}
	if task.Status == TaskQueued || task.Status == TaskRunning {
		data.Refresh = true
	}
	renderPage(w, taskTemplate, data)
}

func parseTaskID(path string) (int64, error) {
	idText := strings.TrimPrefix(path, "/tasks/")
	if idText == "" || strings.Contains(idText, "/") {
		return 0, fmt.Errorf("无效任务编号")
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("无效任务编号")
	}
	return id, nil
}

type homePageData struct {
	Tasks           []Task
	HasActive       bool
	Error           string
	Refresh         bool
	PortfolioStatus string
	PortfolioDetail string
	PortfolioCount  int
}

type taskPageData struct {
	Task         *Task
	CanRecommend bool
	Refresh      bool
}

func renderPage(w http.ResponseWriter, body string, data any) {
	t, err := template.New("page").Funcs(template.FuncMap{
		"shortTime": shortTime,
		"pct":       func(v float64) string { return fmt.Sprintf("%+.2f%%", v) },
		"join":      strings.Join,
	}).Parse(pageTemplate + body)
	if err != nil {
		http.Error(w, "页面模板错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "page", data); err != nil {
		return
	}
}

func shortTime(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) >= 19 {
		return value[:19]
	}
	return value
}

const pageTemplate = `{{define "page"}}<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Refresh}}<meta http-equiv="refresh" content="2">{{end}}
<title>go-quant 本地控制台</title><style>
:root{color:#1f2937;background:#f7f8fa;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}body{max-width:1080px;margin:0 auto;padding:28px 18px 48px}a{color:#155e75;text-decoration:none}header{display:flex;align-items:baseline;justify-content:space-between;border-bottom:1px solid #d7dce1;margin-bottom:22px}h1{font-size:24px;margin:0 0 12px}h2{font-size:18px;margin:26px 0 10px}.card{background:#fff;border:1px solid #dde2e7;border-radius:8px;padding:16px;margin:12px 0}.muted{color:#64748b}.error{color:#b42318}.success{color:#18794e}.running{color:#a16207}button{background:#155e75;color:#fff;border:0;border-radius:6px;padding:9px 13px;cursor:pointer}button:disabled{background:#94a3b8;cursor:not-allowed}table{border-collapse:collapse;width:100%;font-size:14px}th,td{text-align:left;border-bottom:1px solid #e5e7eb;padding:8px 6px;vertical-align:top}th{color:#475569;font-weight:600}.tag{display:inline-block;border-radius:999px;padding:2px 7px;background:#e0f2fe;color:#075985;font-size:12px}.warn{background:#fff7ed;color:#9a3412;padding:10px;border-radius:6px;margin:8px 0}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:12px}.value{font-size:22px;font-weight:650;margin-top:4px}.events{padding-left:18px}.events li{margin:6px 0}@media(max-width:620px){body{padding:18px 12px}table{font-size:12px}th,td{padding:6px 3px}}</style></head>
<body><header><h1><a href="/">go-quant 本地控制台</a></h1><span class="muted">第一期：日终任务与报告</span></header>{{template "body" .}}</body></html>{{end}}`

const homeTemplate = `{{define "body"}}
<section class="card"><h2>日常日终任务</h2><p class="muted">依次刷新日线、涨跌停价、资金流、指数与板块快照，然后生成结构化日报。数据写入任务始终串行执行。</p>
{{if eq .Error "active"}}<p class="error">已有日终任务正在运行或排队，请先等待完成。</p>{{end}}
<form method="post" action="/tasks/daily">{{if .HasActive}}<button disabled>日终任务运行中</button>{{else}}<button type="submit">运行日常日终任务</button>{{end}}</form></section>
<section class="card"><h2>交易流水</h2><p>SQLite 当前保存 {{.PortfolioCount}} 笔有效交易，是 Web 日报的持仓数据来源。</p>
{{if eq .PortfolioStatus "imported"}}<p class="success">YAML 交易流水已成功导入。</p>{{end}}
{{if eq .PortfolioStatus "already-imported"}}<p class="muted">该 YAML 文件已经导入，无需重复操作。</p>{{end}}
{{if or (eq .PortfolioStatus "import-error") (eq .PortfolioStatus "read-error")}}<p class="error">交易流水操作失败：{{.PortfolioDetail}}</p>{{end}}
<form method="post" action="/portfolio/import-yaml" style="display:inline-block;margin-right:8px"><button type="submit">从 portfolio.yaml 导入</button></form><a href="/portfolio/export-yaml">导出 YAML</a>
<p class="muted">导入只允许在 SQLite 流水为空时执行，且同一文件不会重复导入。</p></section>
<section class="card"><h2>最近任务</h2>{{if .Tasks}}<table><thead><tr><th>编号</th><th>状态</th><th>创建时间</th><th>进度 / 结果</th></tr></thead><tbody>{{range .Tasks}}<tr><td><a href="/tasks/{{.ID}}">#{{.ID}}</a></td><td><span class="tag">{{.Status}}</span></td><td>{{shortTime .CreatedAt}}</td><td>{{.Message}}{{if .Error}}<div class="error">{{.Error}}</div>{{end}}</td></tr>{{end}}</tbody></table>{{else}}<p class="muted">还没有任务。首次点击上方按钮即可运行。</p>{{end}}</section>
<section class="card"><h2>本期范围</h2><p class="muted">当前仅允许本机访问的日终任务与报告浏览。持仓录入、交易流水和 AI 问答将在报告数据稳定后进入下一期。</p></section>{{end}}`

const taskTemplate = `{{define "body"}}
<p><a href="/">← 返回任务列表</a></p><section class="card"><h2>日终任务 #{{.Task.ID}}</h2><p>状态：<span class="tag">{{.Task.Status}}</span>　创建：{{shortTime .Task.CreatedAt}}　开始：{{shortTime .Task.StartedAt}}　结束：{{shortTime .Task.FinishedAt}}</p><p>{{.Task.Message}}</p>{{if .Task.Error}}<p class="error">失败原因：{{.Task.Error}}</p>{{end}}{{if .Refresh}}<p class="running">任务执行中，页面每 2 秒自动刷新。</p>{{end}}</section>
<section class="card"><h2>执行记录</h2><ol class="events">{{range .Task.Events}}<li><span class="muted">{{shortTime .CreatedAt}}</span>　{{.Message}}</li>{{end}}</ol></section>
{{with .Task.Report}}<section class="card"><h2>日报（{{.TradeDate}}）</h2><p class="muted">目标日期 {{.TargetDate}} · 生成于 {{shortTime .GeneratedAt.String}}</p>{{if .Warnings}}{{range .Warnings}}<div class="warn">{{.}}</div>{{end}}{{end}}
<div class="grid"><div><span class="muted">仓位策略</span><div class="value">{{.Position.Action}}</div><div>{{.Position.Advice}}</div></div>{{with .Market}}<div><span class="muted">{{.IndexCode}}</span><div class="value">{{printf "%.2f" .IndexClose}} <small>{{pct .IndexChg}}</small></div><div>{{.MATrend}} · 赚钱效应 {{printf "%.1f%%" .ProfitEffect}}</div></div>{{end}}{{with .Intraday}}<div><span class="muted">盘中快照</span><div class="value">{{printf "%.1f%%" .CoveragePct}}</div><div>上涨 {{.RisingCount}} / 下跌 {{.FallingCount}} · {{if .Complete}}覆盖完整{{else}}仅供参考{{end}}</div></div>{{end}}</div>
<h2>正式推荐买入</h2>{{if $.CanRecommend}}{{if .Recommendations}}<table><thead><tr><th>周期</th><th>股票</th><th>建议</th><th>置信度</th><th>建议仓位</th><th>理由</th></tr></thead><tbody>{{range .Recommendations}}<tr><td>{{.Horizon}}</td><td>{{.Code}} {{.Name}}</td><td>{{.Recommendation}}</td><td>{{printf "%.0f%%" .Confidence}}</td><td>{{printf "%.1f%%" .PositionPct}}</td><td>{{join .Reasons "；"}}</td></tr>{{end}}</tbody></table>{{else}}<p>无</p>{{end}}{{else}}<p>无（当前仓位策略为 {{.Position.Action}}，不强行推荐买入。）</p>{{end}}
<h2>观察机会</h2>{{if .Watchlist}}<table><thead><tr><th>周期</th><th>股票</th><th>状态</th><th>置信度</th><th>观察理由</th></tr></thead><tbody>{{range .Watchlist}}<tr><td>{{.Horizon}}</td><td>{{.Code}} {{.Name}}</td><td>可跟踪 / 等待确认</td><td>{{printf "%.0f%%" .Confidence}}</td><td>{{join .Reasons "；"}}</td></tr>{{end}}</tbody></table>{{else}}<p>无</p>{{end}}
{{if .Holdings}}<h2>持仓</h2><table><thead><tr><th>股票</th><th>股数</th><th>成本</th><th>现价</th><th>浮动盈亏</th></tr></thead><tbody>{{range .Holdings}}<tr><td>{{.Code}} {{.Name}}</td><td>{{printf "%.0f" .Shares}}</td><td>{{printf "%.2f" .Cost}}</td><td>{{printf "%.2f" .LastPrice}}</td><td>{{pct .PnLPct}}</td></tr>{{end}}</tbody></table>{{end}}
{{if .News}}<h2>新闻热度</h2><p>近 7 日新闻 {{.News.TotalNews}} 条。{{range .News.HotTopics}}<span class="tag">{{.Keyword}} ×{{.Count}}</span> {{end}}</p>{{end}}
{{if .Sectors}}<h2>板块快照</h2><table><thead><tr><th>板块</th><th>涨跌幅</th><th>宽度</th><th>标签</th></tr></thead><tbody>{{range .Sectors}}<tr><td>{{.SectorName}}</td><td>{{pct .Chg1}}</td><td>{{printf "%.0f%%" .Breadth}}</td><td>{{.Tags}}</td></tr>{{end}}</tbody></table>{{end}}
</section>{{end}}{{end}}`
