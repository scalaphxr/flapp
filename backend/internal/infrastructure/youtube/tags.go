package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Автоподбор тегов для type-beat видео. Два источника:
//   1. Шаблонное ядро — связки, по которым битмейкеров реально находят
//      («{artist} type beat», «free {artist} type beat», год и т.п.).
//   2. Живой спрос — публичный suggest-эндпоинт YouTube (тот же, что кормит
//      автодополнение поиска): реальные популярные запросы вокруг артиста,
//      включая кросс-запросы «artist x artist». Без API-ключа и квот.
// Сеть недоступна — остаётся шаблонное ядро, функция никогда не падает.

const (
	// Лимиты YouTube Data API: тег ≤ 100 символов; суммарная длина ≤ 500,
	// причём тег с пробелом считается в кавычках (+2 символа). Держим запас.
	maxTagLen   = 100
	tagBudget   = 470
	maxTagCount = 40

	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

var suggestHTTP = &http.Client{Timeout: 4 * time.Second}

// GenerateTags builds a prioritised, deduplicated tag list for the given
// "type" artists. Порядок = приоритет: ядро по артистам, базовые
// высокочастотные, подсказки YouTube, добивка общими.
func GenerateTags(ctx context.Context, artists []string) []string {
	year := time.Now().Year()
	b := &tagBuilder{seen: map[string]bool{}}

	clean := make([]string, 0, len(artists))
	for _, a := range artists {
		if a = strings.TrimSpace(a); a != "" {
			clean = append(clean, a)
		}
	}

	for _, a := range clean {
		b.add(a + " type beat")
		b.add("free " + a + " type beat")
		b.add(fmt.Sprintf("%s type beat %d", a, year))
		b.add(a + " instrumental")
		b.add(a + " beats")
		b.add(a)
	}
	for _, t := range []string{"type beat", "free type beat", fmt.Sprintf("type beat %d", year), "instrumental"} {
		b.add(t)
	}
	// Подсказки: длинные хвосты («… sad», «… hard») и коллаб-запросы
	// «artist x other» — то, что реально набирают в поиске.
	for _, a := range clean {
		for _, s := range suggest(ctx, a+" type beat") {
			b.add(s)
		}
	}
	for _, a := range clean {
		for _, s := range suggest(ctx, a+" x ") {
			if strings.Contains(strings.ToLower(s), "type beat") {
				b.add(s)
			}
		}
	}
	for _, t := range []string{"rap instrumental", "trap instrumental", "free beats", "instrumental beats", "beats", "beat"} {
		b.add(t)
	}
	return b.tags
}

// tagBuilder accumulates tags preserving insertion order under the YouTube
// length budget; дубликаты сравниваются без учёта регистра.
type tagBuilder struct {
	seen  map[string]bool
	tags  []string
	total int
}

func (b *tagBuilder) add(t string) {
	t = strings.ToLower(strings.Join(strings.Fields(t), " "))
	if t == "" || len(t) > maxTagLen || b.seen[t] || len(b.tags) >= maxTagCount {
		return
	}
	cost := len(t)
	if strings.Contains(t, " ") {
		cost += 2 // неявные кавычки в счётчике YouTube
	}
	if b.total+cost > tagBudget {
		return
	}
	b.seen[t] = true
	b.tags = append(b.tags, t)
	b.total += cost
}

// suggest queries the public YouTube autocomplete endpoint. Ошибки сети/парсинга
// не всплывают: подсказки — опциональный источник.
func suggest(ctx context.Context, q string) []string {
	u := "https://suggestqueries.google.com/complete/search?client=firefox&ds=yt&hl=en&gl=US&q=" + url.QueryEscape(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := suggestHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	// Формат: ["запрос", ["подсказка", ...], ...]
	var raw []json.RawMessage
	if json.Unmarshal(body, &raw) != nil || len(raw) < 2 {
		return nil
	}
	var out []string
	if json.Unmarshal(raw[1], &out) != nil {
		return nil
	}
	return out
}
