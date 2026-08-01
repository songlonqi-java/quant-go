// Package web exposes the local, single-user Web interface. It binds only to
// loopback by default; authentication and public deployment belong to a later
// phase rather than being implied by this development server.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quant/internal/config"
	"quant/internal/portfolio"
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
	csrfToken     string
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
	csrfToken, err := newCSRFToken()
	if err != nil {
		store.close()
		return nil, fmt.Errorf("生成表单安全令牌: %w", err)
	}
	server := &Server{store: store, portfolioPath: opts.PortfolioPath, csrfToken: csrfToken}
	if execute == nil {
		execute = defaultExecutor(opts, store)
	}
	server.runner = newTaskRunner(store, execute)
	server.mux = http.NewServeMux()
	server.mux.HandleFunc("GET /healthz", server.handleHealth)
	server.mux.HandleFunc("POST /tasks/daily", server.handleCreateDaily)
	server.mux.HandleFunc("POST /portfolio/import-yaml", server.handlePortfolioImport)
	server.mux.HandleFunc("GET /portfolio/export-yaml", server.handlePortfolioExport)
	server.mux.HandleFunc("GET /portfolio", server.handlePortfolio)
	server.mux.HandleFunc("POST /portfolio/transactions", server.handlePortfolioCreate)
	server.mux.HandleFunc("POST /portfolio/transactions/{id}/comment", server.handlePortfolioComment)
	server.mux.HandleFunc("POST /portfolio/transactions/{id}/void", server.handlePortfolioVoid)
	server.mux.HandleFunc("GET /reports", server.handleReports)
	server.mux.HandleFunc("GET /reports/{id}", server.handleReport)
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
		report := reportFromDaily(result)
		report.SnapshotLedger = append([]portfolio.Transaction(nil), ledger.Transactions...)
		return report, err
	}
}

func (s *Server) handlePortfolioImport(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
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

func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	transactions, err := s.store.portfolioTransactions(r.Context(), true)
	if err != nil {
		http.Error(w, "读取交易流水失败", http.StatusInternalServerError)
		return
	}
	ledger, err := s.store.portfolioLedger(r.Context())
	if err != nil {
		http.Error(w, "计算持仓失败", http.StatusInternalServerError)
		return
	}
	audits, err := s.store.portfolioAudits(r.Context(), 30)
	if err != nil {
		http.Error(w, "读取审计记录失败", http.StatusInternalServerError)
		return
	}
	report, err := s.store.latestDailyReport(r.Context())
	if err != nil {
		http.Error(w, "读取最近日报失败", http.StatusInternalServerError)
		return
	}
	data := portfolioPageData{
		Transactions: transactions,
		Audits:       audits,
		CSRFToken:    s.csrfToken,
		Status:       r.URL.Query().Get("status"),
		Error:        r.URL.Query().Get("error"),
	}
	data.Holdings = portfolioHoldingViews(ledger, report)
	data.ClosedTrades = ledger.ClosedTrades()
	renderPage(w, portfolioTemplate, data)
}

func (s *Server) handlePortfolioCreate(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
	shares, sharesErr := strconv.ParseFloat(r.FormValue("shares"), 64)
	price, priceErr := strconv.ParseFloat(r.FormValue("price"), 64)
	if sharesErr != nil || priceErr != nil {
		redirectPortfolioError(w, r, fmt.Errorf("股数和价格必须是数字"))
		return
	}
	_, err := s.store.createPortfolioTransaction(r.Context(), portfolio.Transaction{
		Date: r.FormValue("date"), Code: r.FormValue("code"), Action: r.FormValue("action"),
		Shares: shares, Price: price, Comment: r.FormValue("comment"),
	}, "web")
	if err != nil {
		redirectPortfolioError(w, r, err)
		return
	}
	http.Redirect(w, r, "/portfolio?status=created", http.StatusSeeOther)
}

func (s *Server) handlePortfolioComment(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
	id, version, err := portfolioMutationID(r)
	if err == nil {
		_, err = s.store.updatePortfolioComment(r.Context(), id, version, r.FormValue("comment"), "web")
	}
	if err != nil {
		redirectPortfolioError(w, r, err)
		return
	}
	http.Redirect(w, r, "/portfolio?status=updated", http.StatusSeeOther)
}

func (s *Server) handlePortfolioVoid(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
	id, version, err := portfolioMutationID(r)
	if err == nil {
		err = s.store.voidPortfolioTransaction(r.Context(), id, version, "web")
	}
	if err != nil {
		redirectPortfolioError(w, r, err)
		return
	}
	http.Redirect(w, r, "/portfolio?status=voided", http.StatusSeeOther)
}

func portfolioMutationID(r *http.Request) (int64, int, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, 0, fmt.Errorf("无效交易编号")
	}
	version, err := strconv.Atoi(r.FormValue("version"))
	if err != nil || version < 1 {
		return 0, 0, fmt.Errorf("无效交易版本")
	}
	return id, version, nil
}

func redirectPortfolioError(w http.ResponseWriter, r *http.Request, err error) {
	http.Redirect(w, r, "/portfolio?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
}

func (s *Server) validCSRF(r *http.Request) bool {
	provided := r.FormValue("csrf_token")
	return len(provided) == len(s.csrfToken) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.csrfToken)) == 1
}

func newCSRFToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
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

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	filter := ReportFilter{
		Kind:      strings.TrimSpace(r.URL.Query().Get("kind")),
		TradeDate: strings.TrimSpace(r.URL.Query().Get("trade_date")),
		Status:    validReportStatus(r.URL.Query().Get("status")),
		Limit:     100,
	}
	if filter.Kind != "" && filter.Kind != taskKindDaily {
		filter.Kind = ""
	}
	if filter.TradeDate != "" {
		if _, err := time.Parse("20060102", filter.TradeDate); err != nil {
			renderPage(w, reportsTemplate, reportsPageData{Filter: filter, Error: "交易日必须是 YYYYMMDD", Refresh: false})
			return
		}
	}
	reports, err := s.store.reports(r.Context(), filter)
	if err != nil {
		http.Error(w, "读取报告中心失败", http.StatusInternalServerError)
		return
	}
	renderPage(w, reportsTemplate, reportsPageData{Reports: reports, Filter: filter})
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	report, err := s.store.report(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "读取报告失败", http.StatusInternalServerError)
		return
	}
	snapshot, err := s.store.reportSnapshot(r.Context(), report.PortfolioSnapshotID)
	if err != nil {
		http.Error(w, "读取报告持仓快照失败", http.StatusInternalServerError)
		return
	}
	data := reportPageData{Record: report}
	if snapshot != nil {
		data.Snapshot = snapshot.Transactions
	}
	renderPage(w, reportCenterDetailTemplate, data)
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
		CSRFToken:       s.csrfToken,
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
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
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
	CSRFToken       string
}

type portfolioHoldingView struct {
	Code      string
	Name      string
	Shares    float64
	Cost      float64
	LastPrice float64
	MarketVal float64
	PnL       float64
	PnLPct    float64
	PriceAsOf string
	HasPrice  bool
}

type portfolioPageData struct {
	Holdings     []portfolioHoldingView
	ClosedTrades []portfolio.ClosedTrade
	Transactions []StoredTransaction
	Audits       []PortfolioAudit
	CSRFToken    string
	Status       string
	Error        string
	Refresh      bool
}

type reportsPageData struct {
	Reports []ReportRecord
	Filter  ReportFilter
	Error   string
	Refresh bool
}

type reportPageData struct {
	Record   *ReportRecord
	Snapshot []portfolio.Transaction
	Refresh  bool
}

func portfolioHoldingViews(ledger *portfolio.Ledger, report *DailyReport) []portfolioHoldingView {
	if ledger == nil {
		return nil
	}
	prices := make(map[string]portfolio.PositionStatus)
	priceDate := ""
	if report != nil {
		priceDate = report.TradeDate
		for _, holding := range report.Holdings {
			prices[holding.Code] = holding
		}
	}
	holdings := ledger.CurrentHoldings()
	views := make([]portfolioHoldingView, 0, len(holdings))
	for _, holding := range holdings {
		view := portfolioHoldingView{Code: holding.Code, Name: holding.Code, Shares: holding.Shares, Cost: holding.AvgCost}
		if price, ok := prices[holding.Code]; ok && price.LastPrice > 0 {
			view.Name = price.Name
			view.LastPrice = price.LastPrice
			view.MarketVal = price.LastPrice * holding.Shares
			view.PnL = (price.LastPrice - holding.AvgCost) * holding.Shares
			view.PnLPct = (price.LastPrice/holding.AvgCost - 1) * 100
			view.PriceAsOf = priceDate
			view.HasPrice = true
		}
		views = append(views, view)
	}
	return views
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
	var rendered bytes.Buffer
	if err := t.ExecuteTemplate(&rendered, "page", data); err != nil {
		http.Error(w, "页面渲染错误："+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(rendered.Bytes())
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
:root{color:#1f2937;background:#f7f8fa;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}body{max-width:1080px;margin:0 auto;padding:28px 18px 48px}a{color:#155e75;text-decoration:none}header{display:flex;align-items:baseline;justify-content:space-between;border-bottom:1px solid #d7dce1;margin-bottom:22px}nav a{margin-left:14px}h1{font-size:24px;margin:0 0 12px}h2{font-size:18px;margin:26px 0 10px}.card{background:#fff;border:1px solid #dde2e7;border-radius:8px;padding:16px;margin:12px 0}.muted{color:#64748b}.error{color:#b42318}.success{color:#18794e}.running{color:#a16207}button{background:#155e75;color:#fff;border:0;border-radius:6px;padding:9px 13px;cursor:pointer}button:disabled{background:#94a3b8;cursor:not-allowed}.danger{background:#b42318}input,select{border:1px solid #cbd5e1;border-radius:5px;padding:7px;box-sizing:border-box}.trade-form{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;align-items:end}.trade-form label{display:flex;flex-direction:column;gap:4px;font-size:13px}.inline-form{display:flex;gap:6px;align-items:center}.inline-form input[type=text]{min-width:120px;width:100%}table{border-collapse:collapse;width:100%;font-size:14px}th,td{text-align:left;border-bottom:1px solid #e5e7eb;padding:8px 6px;vertical-align:top}th{color:#475569;font-weight:600}.table-wrap{overflow-x:auto}.tag{display:inline-block;border-radius:999px;padding:2px 7px;background:#e0f2fe;color:#075985;font-size:12px}.warn{background:#fff7ed;color:#9a3412;padding:10px;border-radius:6px;margin:8px 0}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:12px}.value{font-size:22px;font-weight:650;margin-top:4px}.events{padding-left:18px}.events li{margin:6px 0}@media(max-width:620px){body{padding:18px 12px}header{display:block}nav{margin-bottom:10px}nav a{margin:0 14px 0 0}table{font-size:12px}th,td{padding:6px 3px}}</style></head>
<body><header><h1><a href="/">go-quant 本地控制台</a></h1><nav><a href="/">任务</a><a href="/reports">报告</a><a href="/portfolio">持仓</a></nav></header>{{template "body" .}}</body></html>{{end}}`

const homeTemplate = `{{define "body"}}
<section class="card"><h2>日常日终任务</h2><p class="muted">依次刷新日线、涨跌停价、资金流、指数与板块快照，然后生成结构化日报。数据写入任务始终串行执行。</p>
{{if eq .Error "active"}}<p class="error">已有日终任务正在运行或排队，请先等待完成。</p>{{end}}
<form method="post" action="/tasks/daily"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}">{{if .HasActive}}<button disabled>日终任务运行中</button>{{else}}<button type="submit">运行日常日终任务</button>{{end}}</form></section>
<section class="card"><h2>交易流水</h2><p>SQLite 当前保存 {{.PortfolioCount}} 笔有效交易，是 Web 日报的持仓数据来源。</p>
{{if eq .PortfolioStatus "imported"}}<p class="success">YAML 交易流水已成功导入。</p>{{end}}
{{if eq .PortfolioStatus "already-imported"}}<p class="muted">该 YAML 文件已经导入，无需重复操作。</p>{{end}}
{{if or (eq .PortfolioStatus "import-error") (eq .PortfolioStatus "read-error")}}<p class="error">交易流水操作失败：{{.PortfolioDetail}}</p>{{end}}
<form method="post" action="/portfolio/import-yaml" style="display:inline-block;margin-right:8px"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><button type="submit">从 portfolio.yaml 导入</button></form><a href="/portfolio/export-yaml">导出 YAML</a>
<p class="muted">导入只允许在 SQLite 流水为空时执行，且同一文件不会重复导入。</p></section>
<section class="card"><h2>最近任务</h2>{{if .Tasks}}<table><thead><tr><th>编号</th><th>状态</th><th>创建时间</th><th>进度 / 结果</th></tr></thead><tbody>{{range .Tasks}}<tr><td><a href="/tasks/{{.ID}}">#{{.ID}}</a></td><td><span class="tag">{{.Status}}</span></td><td>{{shortTime .CreatedAt}}</td><td>{{.Message}}{{if .Error}}<div class="error">{{.Error}}</div>{{end}}</td></tr>{{end}}</tbody></table>{{else}}<p class="muted">还没有任务。首次点击上方按钮即可运行。</p>{{end}}</section>
<section class="card"><h2>当前范围</h2><p class="muted">当前仅允许本机访问，已支持日终任务、报告和持仓流水。AI 问答、定时任务及公网访问仍未开放。</p></section>{{end}}`

const portfolioTemplate = `{{define "body"}}
<h1>持仓与交易流水</h1>
{{if .Error}}<div class="warn">操作失败：{{.Error}}</div>{{end}}
{{if eq .Status "created"}}<p class="success">交易已录入。</p>{{end}}{{if eq .Status "updated"}}<p class="success">备注已更新。</p>{{end}}{{if eq .Status "voided"}}<p class="success">交易已撤销，历史记录仍保留。</p>{{end}}
<section class="card"><h2>录入交易</h2><form class="trade-form" method="post" action="/portfolio/transactions">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<label>交易日期<input name="date" inputmode="numeric" placeholder="YYYYMMDD" maxlength="8" required></label>
<label>股票代码<input name="code" placeholder="000001.SZ" required></label>
<label>方向<select name="action"><option value="buy">买入</option><option value="sell">卖出</option></select></label>
<label>股数<input name="shares" type="number" min="1" step="1" required></label>
<label>成交价<input name="price" type="number" min="0.001" step="0.001" required></label>
<label>备注<input name="comment" maxlength="500"></label><button type="submit">保存交易</button></form>
<p class="muted">卖出不能超过当时持仓；流水按交易日期和录入顺序计算。</p></section>
<section class="card"><h2>当前持仓</h2>{{if .Holdings}}<div class="table-wrap"><table><thead><tr><th>股票</th><th>股数</th><th>成本</th><th>最近价</th><th>市值</th><th>浮动盈亏</th></tr></thead><tbody>{{range .Holdings}}<tr><td>{{.Code}} {{.Name}}</td><td>{{printf "%.0f" .Shares}}</td><td>{{printf "%.3f" .Cost}}</td>{{if .HasPrice}}<td>{{printf "%.3f" .LastPrice}}<div class="muted">{{.PriceAsOf}}</div></td><td>{{printf "%.2f" .MarketVal}}</td><td>{{pct .PnLPct}} / {{printf "%+.2f" .PnL}}</td>{{else}}<td class="muted">运行日报后更新</td><td>-</td><td>-</td>{{end}}</tr>{{end}}</tbody></table></div>{{else}}<p class="muted">当前空仓。</p>{{end}}</section>
<section class="card"><h2>交易流水</h2>{{if .Transactions}}<div class="table-wrap"><table><thead><tr><th>日期</th><th>股票</th><th>方向</th><th>股数</th><th>价格</th><th>备注</th><th>状态</th><th>操作</th></tr></thead><tbody>{{range .Transactions}}<tr><td>{{.Trade.Date}}</td><td>{{.Trade.Code}}</td><td>{{if eq .Trade.Action "buy"}}买入{{else}}卖出{{end}}</td><td>{{printf "%.0f" .Trade.Shares}}</td><td>{{printf "%.3f" .Trade.Price}}</td><td>{{if eq .Status "active"}}<form class="inline-form" method="post" action="/portfolio/transactions/{{.ID}}/comment"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><input type="hidden" name="version" value="{{.Version}}"><input type="text" name="comment" maxlength="500" value="{{.Trade.Comment}}"><button type="submit">保存</button></form>{{else}}{{.Trade.Comment}}{{end}}</td><td><span class="tag">{{.Status}}</span></td><td>{{if eq .Status "active"}}<form method="post" action="/portfolio/transactions/{{.ID}}/void" onsubmit="return confirm('确认撤销这笔交易？记录不会被物理删除。')"><input type="hidden" name="csrf_token" value="{{$.CSRFToken}}"><input type="hidden" name="version" value="{{.Version}}"><button class="danger" type="submit">撤销</button></form>{{else}}-{{end}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">暂无交易流水，可返回首页导入 YAML。</p>{{end}}</section>
{{if .ClosedTrades}}<section class="card"><h2>已平仓明细</h2><div class="table-wrap"><table><thead><tr><th>买入日</th><th>卖出日</th><th>股票</th><th>股数</th><th>收益</th></tr></thead><tbody>{{range .ClosedTrades}}<tr><td>{{.BuyDate}}</td><td>{{.SellDate}}</td><td>{{.Code}}</td><td>{{printf "%.0f" .Shares}}</td><td>{{pct .Return}} / {{printf "%+.2f" .PnL}}</td></tr>{{end}}</tbody></table></div></section>{{end}}
<section class="card"><h2>最近审计记录</h2>{{if .Audits}}<div class="table-wrap"><table><thead><tr><th>时间</th><th>交易编号</th><th>操作</th><th>来源</th></tr></thead><tbody>{{range .Audits}}<tr><td>{{shortTime .CreatedAt}}</td><td>#{{.TransactionID}}</td><td>{{.Operation}}</td><td>{{.Source}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">暂无审计记录。</p>{{end}}</section>{{end}}`

const reportsTemplate = `{{define "body"}}<h1>报告中心</h1>
<section class="card"><form class="trade-form" method="get" action="/reports"><label>报告类型<select name="kind"><option value="">全部</option><option value="daily" {{if eq .Filter.Kind "daily"}}selected{{end}}>日终分析</option></select></label><label>交易日<input name="trade_date" value="{{.Filter.TradeDate}}" placeholder="YYYYMMDD" maxlength="8"></label><label>任务状态<select name="status"><option value="">全部</option><option value="succeeded" {{if eq .Filter.Status "succeeded"}}selected{{end}}>成功</option><option value="failed" {{if eq .Filter.Status "failed"}}selected{{end}}>失败</option></select></label><button type="submit">筛选</button></form>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}</section>
<section class="card">{{if .Reports}}<div class="table-wrap"><table><thead><tr><th>报告</th><th>类型</th><th>状态</th><th>交易日</th><th>数据版本</th><th>策略版本</th><th>代码版本</th><th>持仓快照</th><th>生成时间</th></tr></thead><tbody>{{range .Reports}}<tr><td><a href="/reports/{{.ID}}">#{{.ID}}</a><div class="muted">任务 <a href="/tasks/{{.TaskID}}">#{{.TaskID}}</a></div></td><td>{{.Kind}}</td><td><span class="tag">{{.TaskStatus}}</span></td><td>{{.TradeDate}}</td><td>{{.DataVersion}}</td><td>{{.StrategyVersion}}</td><td>{{.CodeVersion}}</td><td>{{.SnapshotTransactions}} 笔</td><td>{{shortTime .GeneratedAt}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">没有符合条件的报告。</p>{{end}}</section>{{end}}`

const reportCenterDetailTemplate = `{{define "body"}}<p><a href="/reports">← 返回报告中心</a></p><section class="card"><h1>报告 #{{.Record.ID}}</h1><div class="grid"><div><span class="muted">任务</span><div><a href="/tasks/{{.Record.TaskID}}">#{{.Record.TaskID}}</a> · {{.Record.TaskStatus}}</div></div><div><span class="muted">交易日 / 数据版本</span><div>{{.Record.TradeDate}} / {{.Record.DataVersion}}</div></div><div><span class="muted">报告 / 代码版本</span><div>{{.Record.ReportVersion}} / {{.Record.CodeVersion}}</div></div><div><span class="muted">策略版本</span><div>{{.Record.StrategyVersion}}</div></div><div><span class="muted">持仓快照</span><div>{{.Record.SnapshotTransactions}} 笔交易</div></div></div></section>
{{with .Record.Report}}<section class="card"><h2>分析摘要</h2><p>仓位策略：<strong>{{.Position.Action}}</strong>　{{.Position.Advice}}</p><p>正式信号 {{len .Signals}} 条，正式买入 {{len .Recommendations}} 条，观察机会 {{len .Watchlist}} 条。</p>{{if .Warnings}}{{range .Warnings}}<div class="warn">{{.}}</div>{{end}}{{end}}</section>{{end}}
<section class="card"><h2>执行时持仓流水快照</h2>{{if .Snapshot}}<div class="table-wrap"><table><thead><tr><th>日期</th><th>股票</th><th>方向</th><th>股数</th><th>价格</th><th>备注</th></tr></thead><tbody>{{range .Snapshot}}<tr><td>{{.Date}}</td><td>{{.Code}}</td><td>{{.Action}}</td><td>{{printf "%.0f" .Shares}}</td><td>{{printf "%.3f" .Price}}</td><td>{{.Comment}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">旧报告没有持仓流水快照。</p>{{end}}</section>{{end}}`

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
