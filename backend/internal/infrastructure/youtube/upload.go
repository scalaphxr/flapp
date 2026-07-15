package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

// UploadMeta is the video metadata sent with videos.insert.
type UploadMeta struct {
	Title       string
	Description string
	Tags        []string
	Privacy     string // public | unlisted | private
}

// UploadVideo pushes an MP4 through the resumable-upload protocol and returns
// the new video id. Прогресс — доля отправленных байт файла.
func (c *Client) UploadVideo(ctx context.Context, videoPath string, meta UploadMeta, progress func(float64)) (string, error) {
	access, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}

	f, err := os.Open(videoPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}

	privacy := meta.Privacy
	switch privacy {
	case "public", "unlisted", "private":
	default:
		privacy = "public"
	}

	payload := map[string]any{
		"snippet": map[string]any{
			"title":       sanitizeTitle(meta.Title),
			"description": meta.Description,
			"tags":        clampTags(meta.Tags),
			"categoryId":  "10", // Music
		},
		"status": map[string]any{
			"privacyStatus":           privacy,
			"selfDeclaredMadeForKids": false,
		},
	}
	metaJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Шаг 1: открыть resumable-сессию, получить upload URL из Location.
	initURL := uploadBase + "/videos?uploadType=resumable&part=snippet,status"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL, bytes.NewReader(metaJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "video/mp4")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", st.Size()))

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload init: %s: %s", resp.Status, truncate(string(body), 400))
	}
	session := resp.Header.Get("Location")
	if session == "" {
		return "", fmt.Errorf("upload init: no session URL")
	}

	// Шаг 2: залить файл одним PUT; ретраи не нужны для локального клиента —
	// при обрыве джоба падает и пользователь запускает заново.
	pr := &progressReader{r: f, total: st.Size(), cb: progress}
	up, err := http.NewRequestWithContext(ctx, http.MethodPut, session, pr)
	if err != nil {
		return "", err
	}
	up.ContentLength = st.Size()
	up.Header.Set("Content-Type", "video/mp4")

	resp2, err := c.http.Do(up)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload: %s: %s", resp2.Status, truncate(string(body2), 400))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body2, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("upload: cannot parse response: %s", truncate(string(body2), 200))
	}
	return out.ID, nil
}

// sanitizeTitle enforces YouTube title rules: no '<' or '>', max 100 chars.
func sanitizeTitle(s string) string {
	s = strings.NewReplacer("<", "‹", ">", "›").Replace(strings.TrimSpace(s))
	if s == "" {
		s = "Untitled beat"
	}
	if utf8.RuneCountInString(s) > 100 {
		r := []rune(s)
		s = string(r[:100])
	}
	return s
}

// clampTags keeps the combined tag length under YouTube's 500-char budget.
func clampTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	total := 0
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// Теги с пробелами YouTube считает в кавычках — +2 к бюджету.
		cost := len(t)
		if strings.Contains(t, " ") {
			cost += 2
		}
		if total+cost > 480 {
			break
		}
		total += cost
		out = append(out, t)
	}
	return out
}

// progressReader reports the fraction of bytes read from the underlying file.
type progressReader struct {
	r     io.Reader
	total int64
	read  int64
	cb    func(float64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		if p.cb != nil && p.total > 0 {
			p.cb(float64(p.read) / float64(p.total))
		}
	}
	return n, err
}
