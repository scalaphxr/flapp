package youtube

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	authEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
	apiBase       = "https://www.googleapis.com/youtube/v3"
	uploadBase    = "https://www.googleapis.com/upload/youtube/v3"
	// Scopes: загрузка видео + чтение канала (имя для UI).
	scopes = "https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.readonly"
)

// token is the persisted OAuth state (dataDir/youtube_token.json).
type token struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"expiry"`
	ChannelTitle string    `json:"channelTitle,omitempty"`
}

// Credentials returns the OAuth client id/secret the user entered in settings.
type Credentials func() (clientID, clientSecret string)

// Client talks to Google OAuth and the YouTube Data API. Токен живёт в файле
// рядом с базой; обновление по refresh_token прозрачно для вызывающего кода.
type Client struct {
	tokenPath string
	creds     Credentials
	http      *http.Client

	mu sync.Mutex // сериализует чтение/запись токен-файла и refresh

	authMu     sync.Mutex
	authCancel context.CancelFunc // активный loopback-флоу (новый старт обрывает старый)
}

// NewClient builds a Client storing its token at tokenPath.
func NewClient(tokenPath string, creds Credentials) *Client {
	return &Client{
		tokenPath: tokenPath,
		creds:     creds,
		// Без Timeout: длинные аплоады управляются контекстом джобы.
		http: &http.Client{},
	}
}

// --- token persistence ---

func (c *Client) loadToken() (*token, error) {
	data, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return nil, err
	}
	var t token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) saveToken(t *token) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.tokenPath), 0o755); err != nil {
		return err
	}
	tmp := c.tokenPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.tokenPath)
}

// Status describes the connection for the settings UI.
type Status struct {
	Configured   bool   `json:"configured"`   // client id/secret заполнены
	Connected    bool   `json:"connected"`    // есть refresh-токен
	ChannelTitle string `json:"channelTitle"` // имя канала (снимок на момент подключения)
}

// Status reports whether credentials are set and a channel is connected.
func (c *Client) Status() Status {
	id, secret := c.creds()
	st := Status{Configured: id != "" && secret != ""}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, err := c.loadToken(); err == nil && t.RefreshToken != "" {
		st.Connected = true
		st.ChannelTitle = t.ChannelTitle
	}
	return st
}

// Disconnect removes the stored token.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := os.Remove(c.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// --- interactive auth (installed app + loopback redirect + PKCE) ---

// StartAuth opens the Google consent screen in the system browser and waits in
// the background for the loopback redirect. Возвращает URL согласия — UI
// показывает его как запасной вариант, если браузер не открылся.
func (c *Client) StartAuth() (string, error) {
	clientID, clientSecret := c.creds()
	if clientID == "" || clientSecret == "" {
		return "", errors.New("youtube: client id/secret are not configured")
	}

	// Прервать предыдущий незавершённый флоу, если пользователь кликнул снова.
	c.authMu.Lock()
	if c.authCancel != nil {
		c.authCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	c.authCancel = cancel
	c.authMu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", port)

	verifier := randomURLSafe(64)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomURLSafe(24)

	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {scopes},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authURL := authEndpoint + "?" + q.Encode()

	go c.waitForCallback(ctx, cancel, ln, redirect, clientID, clientSecret, verifier, state)

	openBrowser(authURL)
	return authURL, nil
}

// waitForCallback serves exactly one redirect request, exchanges the code and
// stores the token.
func (c *Client) waitForCallback(ctx context.Context, cancel context.CancelFunc, ln net.Listener, redirect, clientID, clientSecret, verifier, state string) {
	defer cancel()
	defer ln.Close()

	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if e := q.Get("error"); e != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body style='font-family:sans-serif'><h3>Доступ не выдан: %s</h3>Можно закрыть вкладку.</body></html>", e)
			select {
			case resCh <- result{err: fmt.Errorf("consent denied: %s", e)}:
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "no code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body style='font-family:sans-serif'><h3>YouTube подключён ✓</h3>Вернись в Flapp — эту вкладку можно закрыть.</body></html>")
		select {
		case resCh <- result{code: code}:
		default:
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	select {
	case <-ctx.Done():
		return
	case res := <-resCh:
		if res.err != nil {
			return
		}
		// Обмен кода на токены.
		form := url.Values{
			"client_id":     {clientID},
			"client_secret": {clientSecret},
			"code":          {res.code},
			"code_verifier": {verifier},
			"grant_type":    {"authorization_code"},
			"redirect_uri":  {redirect},
		}
		exCtx, exCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer exCancel()
		tk, err := c.exchange(exCtx, form)
		if err != nil {
			return
		}
		// Имя канала — для строки статуса в настройках.
		if title, err := c.fetchChannelTitle(exCtx, tk.AccessToken); err == nil {
			tk.ChannelTitle = title
		}
		c.mu.Lock()
		_ = c.saveToken(tk)
		c.mu.Unlock()
	}
}

// exchange posts the token endpoint form and parses the response.
func (c *Client) exchange(ctx context.Context, form url.Values) (*token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint: %s: %s", resp.Status, truncate(string(body), 300))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &token{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// accessToken returns a valid access token, refreshing it when expired.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	clientID, clientSecret := c.creds()
	c.mu.Lock()
	defer c.mu.Unlock()
	t, err := c.loadToken()
	if err != nil || t.RefreshToken == "" {
		return "", errors.New("youtube: channel is not connected")
	}
	if time.Until(t.Expiry) > time.Minute && t.AccessToken != "" {
		return t.AccessToken, nil
	}
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {t.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	fresh, err := c.exchange(ctx, form)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}
	// refresh-ответ не содержит refresh_token — сохраняем прежний.
	fresh.RefreshToken = t.RefreshToken
	fresh.ChannelTitle = t.ChannelTitle
	if err := c.saveToken(fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// fetchChannelTitle asks the API for the authorised channel's display name.
func (c *Client) fetchChannelTitle(ctx context.Context, access string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/channels?part=snippet&mine=true", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("channels.list: %s", resp.Status)
	}
	var out struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if len(out.Items) == 0 {
		return "", errors.New("no channel")
	}
	return out.Items[0].Snippet.Title, nil
}

// --- helpers ---

func randomURLSafe(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// openBrowser opens url in the OS default browser (best effort).
func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
