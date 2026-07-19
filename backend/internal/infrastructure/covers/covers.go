// Package covers finds publicly available cover images for a search query
// (обложки type-beat видео) через внутренний веб-API Pinterest. Анонимный
// запрос к API отдаёт 403, пока сессия не «прогрета»: сначала GET главной
// наполняет cookie-jar токеном csrftoken, затем запрос поиска шлёт его же в
// заголовке X-CSRFToken (double-submit CSRF). Всё best effort: вызывающий код
// трактует ошибку как «ничего не нашлось».
package covers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// pinterestAppVersion — значение заголовка X-APP-VERSION. Pinterest не сверяет
// его строго, но без правдоподобного заголовка отвечает капризнее.
const pinterestAppVersion = "0e2d1e8"

// pageSize — сколько пинов просим за один запрос (API отдаёт ~25 максимум).
const pageSize = 25

// maxPages ограничивает пагинацию, чтобы странный ответ не зациклил Search.
const maxPages = 12

// pinterestBase — источник веб-эндпоинтов Pinterest; тесты его переопределяют.
var pinterestBase = "https://www.pinterest.com"

// errForbidden signals a 403 from the search API — обычно протухшая сессия.
var errForbidden = errors.New("pinterest: 403")

// Image is one search hit: превью для сетки и полноразмерный URL.
type Image struct {
	ID     string `json:"id"`
	Thumb  string `json:"thumb"`
	Full   string `json:"full"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Search returns Pinterest pins for the query. httpc обязан иметь cookie-jar
// (см. NewCoverService): jar хранит прогретую сессию между бутстрапом и
// запросами поиска. Пагинация идёт по bookmark, пока не набран limit; ошибка
// или пустой результат означают «ничего не нашлось».
func Search(ctx context.Context, httpc *http.Client, query string, limit int) ([]Image, error) {
	if limit <= 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}
	if httpc.Jar == nil {
		return nil, errors.New("covers: http client requires a cookie jar")
	}
	if err := ensureSession(ctx, httpc); err != nil {
		return nil, err
	}

	var out []Image
	seen := map[string]bool{}
	seenBookmark := map[string]bool{}
	var bookmark string
	retried403 := false

	for page := 0; page < maxPages && len(out) < limit; page++ {
		imgs, next, err := pinterestSearch(ctx, httpc, query, bookmark)
		if errors.Is(err, errForbidden) && !retried403 {
			// Сессия протухла: чистим csrftoken, греем заново, повторяем страницу.
			retried403 = true
			clearSession(httpc)
			if berr := ensureSession(ctx, httpc); berr != nil {
				return capTo(out, limit), berr
			}
			imgs, next, err = pinterestSearch(ctx, httpc, query, bookmark)
		}
		if err != nil {
			if len(out) > 0 {
				break // уже что-то набрали — отдаём, а не рушим весь поиск
			}
			return nil, err
		}
		for _, im := range imgs {
			if seen[im.Full] {
				continue
			}
			seen[im.Full] = true
			out = append(out, im)
			if len(out) >= limit {
				break
			}
		}
		if next == "" || seenBookmark[next] {
			break
		}
		seenBookmark[next] = true
		bookmark = next
	}
	return capTo(out, limit), nil
}

// capTo trims imgs to limit.
func capTo(imgs []Image, limit int) []Image {
	if len(imgs) > limit {
		return imgs[:limit]
	}
	return imgs
}

// --- session ---

// csrfToken returns the csrftoken cookie the jar holds for Pinterest, or "".
func csrfToken(httpc *http.Client) string {
	base, err := url.Parse(pinterestBase)
	if err != nil || httpc.Jar == nil {
		return ""
	}
	for _, c := range httpc.Jar.Cookies(base) {
		if c.Name == "csrftoken" {
			return c.Value
		}
	}
	return ""
}

// ensureSession warms the jar with a csrftoken if it doesn't already have one.
func ensureSession(ctx context.Context, httpc *http.Client) error {
	if csrfToken(httpc) != "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pinterestBase+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	// Тело не нужно — cookie-jar уже наполнен из заголовков ответа.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	if csrfToken(httpc) == "" {
		return errors.New("covers: no csrftoken after bootstrap")
	}
	return nil
}

// clearSession drops the csrftoken cookie so the next ensureSession re-bootstraps.
func clearSession(httpc *http.Client) {
	base, err := url.Parse(pinterestBase)
	if err != nil || httpc.Jar == nil {
		return
	}
	httpc.Jar.SetCookies(base, []*http.Cookie{{Name: "csrftoken", Value: "", Path: "/", MaxAge: -1}})
}

// --- search API ---

// pinterestSearch fetches one page of pins and the bookmark for the next page.
func pinterestSearch(ctx context.Context, httpc *http.Client, query, bookmark string) ([]Image, string, error) {
	options := map[string]any{"query": query, "scope": "pins", "page_size": pageSize}
	if bookmark != "" {
		options["bookmarks"] = []string{bookmark}
	}
	dataJSON, err := json.Marshal(map[string]any{"options": options, "context": map[string]any{}})
	if err != nil {
		return nil, "", err
	}

	sourceURL := "/search/pins/?q=" + url.QueryEscape(query)
	u := pinterestBase + "/resource/BaseSearchResource/get/?source_url=" +
		url.QueryEscape(sourceURL) + "&data=" + url.QueryEscape(string(dataJSON))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json, text/javascript, */*, q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-CSRFToken", csrfToken(httpc))
	req.Header.Set("X-APP-VERSION", pinterestAppVersion)
	req.Header.Set("X-Pinterest-PWS-Handler", "www/search/[scope].js")
	req.Header.Set("Referer", pinterestBase+sourceURL)

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, "", errForbidden
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("pinterest: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}

	var parsed struct {
		ResourceResponse struct {
			Bookmark string `json:"bookmark"`
			Data     struct {
				Results []struct {
					ID     string `json:"id"`
					Images map[string]struct {
						URL    string `json:"url"`
						Width  int    `json:"width"`
						Height int    `json:"height"`
					} `json:"images"`
				} `json:"results"`
			} `json:"data"`
		} `json:"resource_response"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "", err
	}

	var imgs []Image
	for _, r := range parsed.ResourceResponse.Data.Results {
		orig, ok := r.Images["orig"]
		if !ok || orig.URL == "" {
			continue // без полноразмерной картинки пин бесполезен
		}
		thumb := orig.URL
		if t, ok := r.Images["236x"]; ok && t.URL != "" {
			thumb = t.URL
		}
		imgs = append(imgs, Image{ID: r.ID, Thumb: thumb, Full: orig.URL, Width: orig.Width, Height: orig.Height})
	}
	return imgs, parsed.ResourceResponse.Bookmark, nil
}

// --- download ---

// Download fetches an image URL into destDir and returns the local file path.
// Только https и не-приватные хосты: эндпоинт не должен быть проксёй внутрь
// локальной сети; тип содержимого должен быть картинкой.
func Download(ctx context.Context, httpc *http.Client, rawURL, destDir string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || host == "" || host == "localhost" {
		return "", errors.New("covers: unsupported image url")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return "", errors.New("covers: unsupported image url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("covers: download: %s", resp.Status)
	}

	ctype := resp.Header.Get("Content-Type")
	ext := strings.ToLower(filepath.Ext(u.Path))
	imageExt := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true}
	if !strings.HasPrefix(ctype, "image/") && !imageExt[ext] {
		return "", fmt.Errorf("covers: not an image: %s", ctype)
	}
	if !imageExt[ext] {
		ext = ".jpg"
		if exts, _ := mime.ExtensionsByType(ctype); len(exts) > 0 {
			ext = exts[0]
		}
	}

	sum := sha1.Sum([]byte(rawURL))
	name := hex.EncodeToString(sum[:8]) + ext

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, name)
	// Уже скачивали — обложки контент-адресуемые, повторная загрузка не нужна.
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 25<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dest, nil
}
