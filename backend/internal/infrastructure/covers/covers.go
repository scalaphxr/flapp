// Package covers finds publicly available cover images for a search query
// (обложки type-beat видео). Приоритетный контент — пины Pinterest, но их
// внутренний API для анонимных запросов закрыт (403), поэтому основной
// рабочий источник — выдача Bing Images по запросу с добавкой «pinterest»:
// сами картинки там в большинстве отдаёт CDN Pinterest (i.pinimg.com).
// Порядок попыток: API Pinterest (вдруг снова откроют) → Bing → og:image со
// страницы поиска Pinterest. Всё best effort: вызывающий код трактует ошибку
// как «ничего не нашлось».
package covers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Image is one search hit: превью для сетки и полноразмерный URL.
type Image struct {
	ID     string `json:"id"`
	Thumb  string `json:"thumb"`
	Full   string `json:"full"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Search returns cover images for the query, пины Pinterest — в начале списка.
func Search(ctx context.Context, httpc *http.Client, query string, limit int) ([]Image, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	if imgs, err := pinterestResource(ctx, httpc, query, limit); err == nil && len(imgs) > 0 {
		return imgs, nil
	}
	if imgs, err := bingImages(ctx, httpc, query, limit); err == nil && len(imgs) > 0 {
		return imgs, nil
	}
	return pinterestOG(ctx, httpc, query)
}

// --- Bing Images (основной рабочий источник) ---

// mAttrRe matches the HTML-escaped JSON blob Bing attaches to every result
// tile: m="{&quot;murl&quot;:&quot;…&quot;,&quot;turl&quot;:&quot;…&quot;,…}".
var mAttrRe = regexp.MustCompile(`\sm="(\{[^"]*\})"`)

func bingImages(ctx context.Context, httpc *http.Client, query string, limit int) ([]Image, error) {
	// «pinterest» в запросе смещает выдачу к пинам; сами файлы придут с
	// i.pinimg.com и попадут в начало списка при сортировке ниже.
	q := query
	if !strings.Contains(strings.ToLower(q), "pinterest") {
		q += " pinterest"
	}
	count := limit * 2
	if count > 100 {
		count = 100
	}
	u := fmt.Sprintf("https://www.bing.com/images/search?q=%s&first=1&count=%d&safeSearch=Moderate", url.QueryEscape(q), count)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bing: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, err
	}

	var pins, rest []Image
	seen := map[string]bool{}
	for _, m := range mAttrRe.FindAllStringSubmatch(string(body), -1) {
		var meta struct {
			MURL string `json:"murl"`
			TURL string `json:"turl"`
		}
		if json.Unmarshal([]byte(html.UnescapeString(m[1])), &meta) != nil {
			continue
		}
		mu, err := url.Parse(meta.MURL)
		if err != nil || (mu.Scheme != "https" && mu.Scheme != "http") || seen[meta.MURL] {
			continue
		}
		seen[meta.MURL] = true
		thumb := meta.TURL
		if thumb == "" {
			thumb = meta.MURL
		}
		sum := sha1.Sum([]byte(meta.MURL))
		img := Image{ID: hex.EncodeToString(sum[:8]), Thumb: thumb, Full: meta.MURL}
		if strings.HasSuffix(strings.ToLower(mu.Hostname()), "pinimg.com") {
			pins = append(pins, img)
		} else {
			rest = append(rest, img)
		}
	}
	imgs := append(pins, rest...)
	if len(imgs) > limit {
		imgs = imgs[:limit]
	}
	if len(imgs) == 0 {
		return nil, errors.New("bing: no images in page")
	}
	return imgs, nil
}

// --- Pinterest (best effort: API закрыт для анонимов, но дёшево проверить) ---

func pinterestResource(ctx context.Context, httpc *http.Client, query string, limit int) ([]Image, error) {
	data := fmt.Sprintf(`{"options":{"query":%q,"scope":"pins","page_size":%d},"context":{}}`, query, limit)
	u := "https://www.pinterest.com/resource/BaseSearchResource/get/?source_url=" +
		url.QueryEscape("/search/pins/?q="+url.QueryEscape(query)) +
		"&data=" + url.QueryEscape(data)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pinterest: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var out struct {
		ResourceResponse struct {
			Data struct {
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
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}

	imgs := make([]Image, 0, limit)
	for _, r := range out.ResourceResponse.Data.Results {
		full, ok := r.Images["orig"]
		if !ok || full.URL == "" {
			continue
		}
		thumb := full.URL
		if t, ok := r.Images["236x"]; ok && t.URL != "" {
			thumb = t.URL
		}
		imgs = append(imgs, Image{ID: r.ID, Thumb: thumb, Full: full.URL, Width: full.Width, Height: full.Height})
		if len(imgs) >= limit {
			break
		}
	}
	return imgs, nil
}

// pinImgRe matches i.pinimg.com CDN links (og:image на странице поиска).
var pinImgRe = regexp.MustCompile(`https://i\.pinimg\.com/(?:originals|736x|564x|236x)/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{2}/[0-9a-f]{32}\.(?:jpg|jpeg|png|webp|gif)`)

// pinterestOG scrapes whatever images the public search page still exposes.
// Обычно это одна og:image — лучше, чем ничего, когда остальное недоступно.
func pinterestOG(ctx context.Context, httpc *http.Client, query string) ([]Image, error) {
	u := "https://www.pinterest.com/search/pins/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pinterest: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var imgs []Image
	for _, m := range pinImgRe.FindAllString(string(body), -1) {
		key := m[strings.LastIndex(m, "/")+1:]
		if seen[key] {
			continue
		}
		seen[key] = true
		imgs = append(imgs, Image{ID: strings.TrimSuffix(key, filepath.Ext(key)), Thumb: m, Full: m})
	}
	if len(imgs) == 0 {
		return nil, errors.New("covers: no images found")
	}
	return imgs, nil
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
