package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quant/internal/config"
)

func TestRegistrationLoginAndSevenDaySession(t *testing.T) {
	root := t.TempDir()
	server, err := newServer(Options{
		Config: &config.Config{Data: config.DataConfig{
			RawDir: filepath.Join(root, "raw"), MetaDir: filepath.Join(root, "meta"),
		}},
		DatabasePath: filepath.Join(root, "meta", "web.db"),
	}, func(context.Context, string, func(string)) (*TaskResult, error) {
		return analysisTaskResult(&DailyReport{}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status=%d", health.Code)
	}
	protected := httptest.NewRecorder()
	server.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/reports", nil))
	if protected.Code != http.StatusSeeOther || !strings.HasPrefix(protected.Header().Get("Location"), "/login?next=") {
		t.Fatalf("protected status=%d location=%q", protected.Code, protected.Header().Get("Location"))
	}

	shortPassword := postAuthForm(server, "/register", url.Values{
		"csrf_token": {server.csrfToken}, "username": {"alice"},
		"password": {"12345"}, "password_confirm": {"12345"},
	})
	if shortPassword.Code != http.StatusBadRequest || !strings.Contains(shortPassword.Body.String(), "不能少于 6") {
		t.Fatalf("short password status=%d body=%s", shortPassword.Code, shortPassword.Body.String())
	}

	registered := postAuthForm(server, "/register", url.Values{
		"csrf_token": {server.csrfToken}, "username": {"alice"}, "next": {"/reports?kind=daily"},
		"password": {"secret1"}, "password_confirm": {"secret1"},
	})
	if registered.Code != http.StatusSeeOther || registered.Header().Get("Location") != "/reports?kind=daily" {
		t.Fatalf("registration status=%d location=%q body=%s", registered.Code, registered.Header().Get("Location"), registered.Body.String())
	}
	cookies := registered.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	sessionCookie := cookies[0]
	if sessionCookie.Name != sessionCookieName || !sessionCookie.HttpOnly || sessionCookie.MaxAge != int(sessionLifetime.Seconds()) {
		t.Fatalf("session cookie=%+v", sessionCookie)
	}
	remaining := time.Until(sessionCookie.Expires)
	if remaining < sessionLifetime-time.Minute || remaining > sessionLifetime+time.Minute {
		t.Fatalf("session lifetime=%v", remaining)
	}

	var passwordHash string
	if err := server.store.db.QueryRow(`SELECT password_hash FROM web_users WHERE username = 'alice'`).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash == "secret1" || !verifyPassword(passwordHash, "secret1") {
		t.Fatalf("password was not stored as a verifiable hash: %q", passwordHash)
	}

	authenticated := httptest.NewRecorder()
	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/reports", nil)
	authenticatedRequest.AddCookie(sessionCookie)
	server.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}

	wrongLogin := postAuthForm(server, "/login", url.Values{
		"csrf_token": {server.csrfToken}, "username": {"alice"}, "password": {"wrong-password"},
	})
	if wrongLogin.Code != http.StatusUnauthorized || !strings.Contains(wrongLogin.Body.String(), ErrInvalidCredential.Error()) {
		t.Fatalf("wrong login status=%d body=%s", wrongLogin.Code, wrongLogin.Body.String())
	}
	login := postAuthForm(server, "/login", url.Values{
		"csrf_token": {server.csrfToken}, "username": {"ALICE"}, "password": {"secret1"},
	})
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login status=%d cookies=%v body=%s", login.Code, login.Result().Cookies(), login.Body.String())
	}

	if _, err := server.store.db.Exec(`UPDATE web_sessions SET expires_at = ? WHERE token_hash = ?`, time.Now().Add(-time.Minute).Unix(), sessionTokenHash(sessionCookie.Value)); err != nil {
		t.Fatal(err)
	}
	expired := httptest.NewRecorder()
	expiredRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	expiredRequest.AddCookie(sessionCookie)
	server.ServeHTTP(expired, expiredRequest)
	if expired.Code != http.StatusSeeOther || !strings.HasPrefix(expired.Header().Get("Location"), "/login?next=") {
		t.Fatalf("expired status=%d location=%q", expired.Code, expired.Header().Get("Location"))
	}
}

func TestRegistrationRejectsDuplicateUsernameAndUnsafeRedirect(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "web.db")
	store, err := openTaskStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.registerUser(context.Background(), "Alice", "secret1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.registerUser(context.Background(), "alice", "secret2"); err != ErrUsernameTaken {
		t.Fatalf("duplicate error=%v", err)
	}
	token, _, err := store.createSession(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	store, err = openTaskStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	restoredUser, err := store.sessionUser(context.Background(), token)
	if err != nil || restoredUser.Username != "Alice" {
		t.Fatalf("persisted session user=%+v err=%v", restoredUser, err)
	}
	if got := safeAuthNext("https://example.com/steal"); got != "/" {
		t.Fatalf("unsafe next=%q", got)
	}
}

func postAuthForm(server *Server, path string, values url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
