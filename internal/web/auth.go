package web

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	passwordIterations = 600_000
	passwordKeyLength  = 32
	passwordSaltLength = 16
	sessionTokenLength = 32
	sessionCookieName  = "quant_session"
	sessionLifetime    = 7 * 24 * time.Hour
)

var (
	ErrUsernameTaken     = errors.New("用户名已存在")
	ErrInvalidCredential = errors.New("用户名或密码错误")
)

type webUser struct {
	ID       int64
	Username string
}

func validateRegistration(username, password, confirmation string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", fmt.Errorf("用户名不能为空")
	}
	if utf8.RuneCountInString(username) > 50 {
		return "", fmt.Errorf("用户名不能超过 50 个字符")
	}
	for _, value := range username {
		if unicode.IsControl(value) {
			return "", fmt.Errorf("用户名包含无效字符")
		}
	}
	if utf8.RuneCountInString(password) < 6 {
		return "", fmt.Errorf("密码不能少于 6 个字符")
	}
	if password != confirmation {
		return "", fmt.Errorf("两次输入的密码不一致")
	}
	return username, nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations != passwordIterations {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != passwordSaltLength {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) != passwordKeyLength {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func (s *taskStore) registerUser(ctx context.Context, username, password string) (*webUser, error) {
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("生成密码摘要: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO web_users(username, password_hash, created_at) VALUES(?, ?, ?)`,
		username, passwordHash, timestamp())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &webUser{ID: id, Username: username}, nil
}

func (s *taskStore) authenticateUser(ctx context.Context, username, password string) (*webUser, error) {
	username = strings.TrimSpace(username)
	var user webUser
	var passwordHash string
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM web_users WHERE username = ?`, username).
		Scan(&user.ID, &user.Username, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, err
	}
	if !verifyPassword(passwordHash, password) {
		return nil, ErrInvalidCredential
	}
	return &user, nil
}

func (s *taskStore) createSession(ctx context.Context, userID int64) (string, time.Time, error) {
	buffer := make([]byte, sessionTokenLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	expiresAt := time.Now().UTC().Add(sessionLifetime)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM web_sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO web_sessions(token_hash, user_id, created_at, expires_at) VALUES(?, ?, ?, ?)`,
		sessionTokenHash(token), userID, timestamp(), expiresAt.Unix()); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *taskStore) sessionUser(ctx context.Context, token string) (*webUser, error) {
	if len(token) < 32 || len(token) > 128 {
		return nil, sql.ErrNoRows
	}
	var user webUser
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username
		FROM web_sessions s JOIN web_users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, sessionTokenHash(token), time.Now().Unix()).
		Scan(&user.ID, &user.Username)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func sessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *Server) serveAuthenticated(w http.ResponseWriter, r *http.Request) {
	if publicAuthPath(r.URL.Path) {
		s.mux.ServeHTTP(w, r)
		return
	}
	user, err := s.requestUser(r)
	if err != nil {
		http.Error(w, "验证登录状态失败", http.StatusInternalServerError)
		return
	}
	if user == nil {
		next := "/"
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next = r.URL.RequestURI()
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func publicAuthPath(path string) bool {
	switch path {
	case "/healthz", "/login", "/register":
		return true
	default:
		return false
	}
}

func (s *Server) requestUser(r *http.Request) (*webUser, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	user, err := s.store.sessionUser(r.Context(), cookie.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.redirectAuthenticated(w, r) {
		return
	}
	renderAuthPage(w, authPageData{
		Title: "登录", Action: "/login", SubmitLabel: "登录", CSRFToken: s.csrfToken,
		Next: safeAuthNext(r.URL.Query().Get("next")),
	}, http.StatusOK)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
	next := safeAuthNext(r.FormValue("next"))
	user, err := s.store.authenticateUser(r.Context(), r.FormValue("username"), r.FormValue("password"))
	if errors.Is(err, ErrInvalidCredential) {
		renderAuthPage(w, authPageData{
			Title: "登录", Action: "/login", SubmitLabel: "登录", CSRFToken: s.csrfToken,
			Next: next, Error: ErrInvalidCredential.Error(), Username: strings.TrimSpace(r.FormValue("username")),
		}, http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	if err := s.startUserSession(w, r, user.ID); err != nil {
		http.Error(w, "创建登录会话失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if s.redirectAuthenticated(w, r) {
		return
	}
	renderAuthPage(w, authPageData{
		Title: "注册", Action: "/register", SubmitLabel: "注册并登录", Register: true,
		CSRFToken: s.csrfToken, Next: safeAuthNext(r.URL.Query().Get("next")),
	}, http.StatusOK)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		http.Error(w, "表单已过期，请刷新页面后重试", http.StatusForbidden)
		return
	}
	next := safeAuthNext(r.FormValue("next"))
	username, err := validateRegistration(r.FormValue("username"), r.FormValue("password"), r.FormValue("password_confirm"))
	if err != nil {
		renderAuthPage(w, authPageData{
			Title: "注册", Action: "/register", SubmitLabel: "注册并登录", Register: true,
			CSRFToken: s.csrfToken, Next: next, Error: err.Error(), Username: strings.TrimSpace(r.FormValue("username")),
		}, http.StatusBadRequest)
		return
	}
	user, err := s.store.registerUser(r.Context(), username, r.FormValue("password"))
	if errors.Is(err, ErrUsernameTaken) {
		renderAuthPage(w, authPageData{
			Title: "注册", Action: "/register", SubmitLabel: "注册并登录", Register: true,
			CSRFToken: s.csrfToken, Next: next, Error: err.Error(), Username: username,
		}, http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "注册失败", http.StatusInternalServerError)
		return
	}
	if err := s.startUserSession(w, r, user.ID); err != nil {
		http.Error(w, "创建登录会话失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) redirectAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	user, err := s.requestUser(r)
	if err != nil {
		http.Error(w, "验证登录状态失败", http.StatusInternalServerError)
		return true
	}
	if user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return true
	}
	return false
}

func (s *Server) startUserSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expiresAt, err := s.store.createSession(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionLifetime.Seconds()), Expires: expiresAt,
	})
	return nil
}

func safeAuthNext(value string) string {
	if value == "" {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "/"
	}
	return parsed.RequestURI()
}

type authPageData struct {
	Title       string
	Action      string
	SubmitLabel string
	Username    string
	Error       string
	CSRFToken   string
	Next        string
	Register    bool
}

func renderAuthPage(w http.ResponseWriter, data authPageData, status int) {
	page, err := template.New("auth").Parse(authTemplate)
	if err != nil {
		http.Error(w, "页面模板错误", http.StatusInternalServerError)
		return
	}
	var rendered bytes.Buffer
	if err := page.Execute(&rendered, data); err != nil {
		http.Error(w, "页面渲染错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(rendered.Bytes())
}

const authTemplate = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · go-quant</title><style>
:root{color:#1f2937;background:#f7f8fa;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
body{margin:0;padding:20px}.auth{max-width:380px;margin:10vh auto;background:#fff;border:1px solid #dde2e7;border-radius:10px;padding:24px}
.auth h1{font-size:24px;margin:0 0 18px}.auth label{display:flex;flex-direction:column;gap:5px;margin:12px 0;font-size:14px}
.auth input{border:1px solid #cbd5e1;border-radius:6px;padding:10px}.auth button{width:100%;background:#155e75;color:#fff;border:0;border-radius:6px;padding:10px;cursor:pointer}
.error{background:#fef2f2;color:#b42318;padding:10px;border-radius:6px}.muted{color:#64748b;font-size:14px}a{color:#155e75;text-decoration:none}
</style></head><body><main class="auth"><h1>go-quant {{.Title}}</h1>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<form method="post" action="{{.Action}}"><input type="hidden" name="csrf_token" value="{{.CSRFToken}}"><input type="hidden" name="next" value="{{.Next}}">
<label>用户名<input name="username" value="{{.Username}}" autocomplete="username" maxlength="50" required autofocus></label>
<label>密码<input name="password" type="password" autocomplete="{{if .Register}}new-password{{else}}current-password{{end}}" minlength="6" required></label>
{{if .Register}}<label>确认密码<input name="password_confirm" type="password" autocomplete="new-password" minlength="6" required></label>{{end}}
<button type="submit">{{.SubmitLabel}}</button></form>
{{if .Register}}<p class="muted">已有账户？<a href="/login?next={{urlquery .Next}}">直接登录</a></p>
{{else}}<p class="muted">还没有账户？<a href="/register?next={{urlquery .Next}}">注册</a></p>{{end}}
{{if .Register}}<p class="muted">注册后可访问全部任务、报告和持仓数据。</p>{{end}}
<p class="muted">登录状态保留 7 天。</p></main></body></html>`
