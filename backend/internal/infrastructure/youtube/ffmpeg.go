// Package youtube renders still-image videos with ffmpeg and uploads them to
// YouTube over the Data API v3 (OAuth 2.0 installed-app flow + resumable
// upload). Протокол покрывается парой HTTP-вызовов, поэтому обходимся без
// официального SDK и его дерева зависимостей.
package youtube

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// ffmpegNextTo returns the bundled ffmpeg.exe sitting in dir, if present. В
// установленном приложении Tauri кладёт externalBin (и сайдкар, и ffmpeg) рядом
// с главным exe, поэтому вшитый ffmpeg лежит в одной папке с сайдкаром.
func ffmpegNextTo(dir string) (string, bool) {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	cand := filepath.Join(dir, name)
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand, true
	}
	return "", false
}

// FindFFmpeg returns the ffmpeg executable to use: the configured override first,
// then the copy bundled next to the sidecar (shipped with the installer), then a
// PATH lookup, then common Windows install locations.
func FindFFmpeg(configured string) (string, error) {
	if configured != "" {
		if st, err := os.Stat(configured); err == nil && !st.IsDir() {
			return configured, nil
		}
	}
	// Вшитый в установку ffmpeg — рядом с исполняемым файлом сайдкара.
	if exe, err := os.Executable(); err == nil {
		if p, ok := ffmpegNextTo(filepath.Dir(exe)); ok {
			return p, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	candidates := []string{`C:\ffmpeg\bin\ffmpeg.exe`}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates, filepath.Join(local, `Microsoft\WinGet\Links\ffmpeg.exe`))
	}
	for _, cand := range candidates {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", errors.New("ffmpeg not found: install it (winget install ffmpeg) or set the path in settings")
}

// ffmpegDownloadURL — официальная портативная сборка gyan.dev (release
// essentials, ~35 МБ): в ней есть libx264 и aac — всё, что нужно рендеру
// «обложка + бит → mp4». Зеркало на GitHub — на случай недоступности сайта.
var ffmpegDownloadURLs = []string{
	"https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip",
	"https://github.com/GyanD/codexffmpeg/releases/latest/download/ffmpeg-release-essentials.zip",
}

// DownloadFFmpeg скачивает портативную сборку и кладёт ffmpeg.exe в destDir,
// возвращая полный путь к бинарнику. Прогресс: 0..0.9 — скачивание,
// 0.9..1 — распаковка.
func DownloadFFmpeg(ctx context.Context, destDir string, progress func(float64)) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(destDir, "ffmpeg-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	var lastErr error
	for _, url := range ffmpegDownloadURLs {
		lastErr = downloadTo(ctx, url, tmp, progress)
		if lastErr == nil {
			break
		}
		// Перед повтором со следующего зеркала файл начинаем заново.
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		if err := tmp.Truncate(0); err != nil {
			return "", err
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("download ffmpeg: %w", lastErr)
	}

	if progress != nil {
		progress(0.9)
	}
	out := filepath.Join(destDir, "ffmpeg.exe")
	if err := extractFFmpeg(tmp.Name(), out); err != nil {
		return "", err
	}
	if progress != nil {
		progress(1)
	}
	return out, nil
}

// downloadTo скачивает url в f, транслируя прогресс 0..0.9.
func downloadTo(ctx context.Context, url string, f *os.File, progress func(float64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}

	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if progress != nil && total > 0 {
				progress(0.9 * float64(done) / float64(total))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// extractFFmpeg достаёт единственный нужный файл (bin/ffmpeg.exe) из zip
// сборки; остальное (документация, ffprobe, ffplay) не распаковывается.
func extractFFmpeg(zipPath, outPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, zf := range zr.File {
		if !strings.EqualFold(filepath.Base(zf.Name), "ffmpeg.exe") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			os.Remove(outPath)
			return err
		}
		return out.Close()
	}
	return errors.New("ffmpeg.exe not found inside the downloaded archive")
}

// durRe matches the input duration ffmpeg prints to stderr, e.g.
// "  Duration: 00:03:12.34, start: ...". Картинка печатает "Duration: N/A",
// поэтому первое числовое совпадение — это длительность аудио.
var durRe = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)

// RenderOpts configures the optional text burned onto the still and an optional
// duration cap (used for quick previews).
type RenderOpts struct {
	Overlay    bool    // рисовать TitleText/SubText поверх кадра
	TitleText  string  // крупная строка (обычно название бита в кавычках)
	SubText    string  // строка помельче под названием (обычно «prod. ник»)
	Font       string  // ключ шрифта (см. FontFiles) или прямой путь к .ttf/.otf; "" = дефолт
	MaxSeconds float64 // >0 — обрезать длительность (для превью); 0 — весь трек
}

// FontFiles maps a font key (used by the UI) to its (bold, regular) files.
// Одновесные шрифты (Impact, Franklin Gothic) используют один файл для обоих.
var FontFiles = map[string][2]string{
	"arial":     {`C:\Windows\Fonts\arialbd.ttf`, `C:\Windows\Fonts\arial.ttf`},
	"impact":    {`C:\Windows\Fonts\impact.ttf`, `C:\Windows\Fonts\impact.ttf`},
	"franklin":  {`C:\Windows\Fonts\framd.ttf`, `C:\Windows\Fonts\framd.ttf`},
	"verdana":   {`C:\Windows\Fonts\verdanab.ttf`, `C:\Windows\Fonts\verdana.ttf`},
	"tahoma":    {`C:\Windows\Fonts\tahomabd.ttf`, `C:\Windows\Fonts\tahoma.ttf`},
	"trebuchet": {`C:\Windows\Fonts\trebucbd.ttf`, `C:\Windows\Fonts\trebuc.ttf`},
	"segoe":     {`C:\Windows\Fonts\segoeuib.ttf`, `C:\Windows\Fonts\segoeui.ttf`},
	"georgia":   {`C:\Windows\Fonts\georgiab.ttf`, `C:\Windows\Fonts\georgia.ttf`},
	"times":     {`C:\Windows\Fonts\timesbd.ttf`, `C:\Windows\Fonts\times.ttf`},
	"comic":     {`C:\Windows\Fonts\comicbd.ttf`, `C:\Windows\Fonts\comic.ttf`},
	"courier":   {`C:\Windows\Fonts\courbd.ttf`, `C:\Windows\Fonts\cour.ttf`},
}

// overlayFontPairs — запасные пары (жирный, обычный) для авто-подбора дефолта.
var overlayFontPairs = [][2]string{
	{`C:\Windows\Fonts\arialbd.ttf`, `C:\Windows\Fonts\arial.ttf`},
	{`C:\Windows\Fonts\segoeuib.ttf`, `C:\Windows\Fonts\segoeui.ttf`},
	{`/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf`, `/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf`},
	{`/System/Library/Fonts/Supplemental/Arial Bold.ttf`, `/System/Library/Fonts/Supplemental/Arial.ttf`},
}

// existingFont returns path if it is a readable file.
func existingFont(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// findOverlayFonts returns the first available (bold, regular) font pair.
func findOverlayFonts() (bold, regular string, ok bool) {
	for _, p := range overlayFontPairs {
		if existingFont(p[0]) {
			reg := p[1]
			if !existingFont(reg) {
				reg = p[0]
			}
			return p[0], reg, true
		}
	}
	return "", "", false
}

// resolveFont turns a font key (or a direct path) into (bold, regular) files,
// falling back to the auto-detected default when the choice is unavailable.
func resolveFont(font string) (bold, regular string, ok bool) {
	if font != "" {
		if existingFont(font) { // прямой путь к файлу шрифта
			return font, font, true
		}
		if pair, has := FontFiles[strings.ToLower(font)]; has && existingFont(pair[0]) {
			reg := pair[1]
			if !existingFont(reg) {
				reg = pair[0]
			}
			return pair[0], reg, true
		}
	}
	return findOverlayFonts()
}

// escFilterPath escapes a filesystem path for use inside an ffmpeg filter option
// value: forward slashes and the drive-letter colon escaped.
func escFilterPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.ReplaceAll(p, ":", `\:`)
	return p
}

// ErrFontMissing reports a font that the caller named explicitly but which is
// not on disk. Рендер в этом случае откатился бы на дефолт молча, и выбранный
// шрифт «просто не применился» — поэтому вызывающий узнаёт об этом явно.
var ErrFontMissing = errors.New("файл шрифта не найден")

// ValidateFont checks that an explicitly named font (key or path) resolves to a
// real file. Пустая строка — это «дефолт», она валидна всегда.
func ValidateFont(font string) error {
	if font == "" {
		return nil
	}
	if existingFont(font) {
		return nil
	}
	if pair, has := FontFiles[strings.ToLower(font)]; has && existingFont(pair[0]) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrFontMissing, font)
}

// buildChain assembles the filtergraph shared by RenderStill and RenderFrame:
// обложка 1080p с сохранением пропорций, фон — размытая копия самой картинки
// (вертикальные обложки не оставляют чёрных полос), опционально название бита
// и ник автора помельче поверх кадра.
//
// Это единственное место, где задана геометрия кадра: и видео, и превью-кадр
// строятся отсюда, поэтому разъехаться они не могут.
//
// Текст пишем во временные файлы (textfile=): так контент не приходится
// экранировать под парсер filtergraph. Их удаляет возвращённый cleanup —
// вызывать обязательно, даже если рендер завершился ошибкой.
func buildChain(opts RenderOpts, tmpPrefix string) (chain string, cleanup func()) {
	var tmps []string
	cleanup = func() {
		for _, p := range tmps {
			os.Remove(p)
		}
	}

	chain = "[0:v]split[bg][fg];" +
		"[bg]scale=1920:1080:force_original_aspect_ratio=increase,crop=1920:1080,gblur=sigma=28,eq=brightness=-0.08[b];" +
		"[fg]scale=1920:1080:force_original_aspect_ratio=decrease[f];" +
		"[b][f]overlay=(W-w)/2:(H-h)/2,format=yuv420p"

	if opts.Overlay {
		if bold, reg, ok := resolveFont(opts.Font); ok {
			if opts.TitleText != "" {
				tf := tmpPrefix + ".title.txt"
				if err := os.WriteFile(tf, []byte(opts.TitleText), 0o644); err == nil {
					tmps = append(tmps, tf)
					chain += fmt.Sprintf(",drawtext=fontfile='%s':textfile='%s':fontcolor=white:fontsize=92:x=(w-text_w)/2:y=h*0.70:shadowcolor=black@0.7:shadowx=3:shadowy=3",
						escFilterPath(bold), escFilterPath(tf))
				}
			}
			if opts.SubText != "" {
				sf := tmpPrefix + ".sub.txt"
				if err := os.WriteFile(sf, []byte(opts.SubText), 0o644); err == nil {
					tmps = append(tmps, sf)
					chain += fmt.Sprintf(",drawtext=fontfile='%s':textfile='%s':fontcolor=white@0.9:fontsize=46:x=(w-text_w)/2:y=h*0.70+120:shadowcolor=black@0.7:shadowx=2:shadowy=2",
						escFilterPath(reg), escFilterPath(sf))
				}
			}
		}
	}
	chain += "[v]"
	return chain, cleanup
}

// RenderFrame renders a single 1920x1080 PNG through the very same filtergraph
// as the final video — это и есть превью кадра в UI. Аудио не нужно, поэтому
// кадр можно показать ещё до выбора трека.
func RenderFrame(ctx context.Context, ffmpeg, imagePath, outPath string, opts RenderOpts) error {
	if opts.Overlay {
		if err := ValidateFont(opts.Font); err != nil {
			return err
		}
	}

	chain, cleanup := buildChain(opts, outPath)
	defer cleanup()

	// Превью показывается в панели ~300px шириной, поэтому полноразмерный кадр
	// 1920×1080 избыточен: он лишь дольше кодируется в PNG и читается в webview.
	// Равномерный даунскейл готового кадра до 960×540 сохраняет ту же геометрию
	// (текст рисуется в полном размере и уменьшается вместе со всем), но вдвое
	// легче — так переключение между битами ощутимо быстрее.
	chain = strings.TrimSuffix(chain, "[v]") + ",scale=960:540[v]"

	args := []string{
		"-y", "-hide_banner",
		"-i", imagePath,
		"-filter_complex", chain,
		"-map", "[v]", "-frames:v", "1",
		"-nostats", outPath,
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	hideWindow(cmd)

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, msg)
	}
	return nil
}

// RenderStill builds an H.264/AAC MP4 from one cover image and one audio
// track: кадр строит buildChain (тот же, что и для превью), 2 fps (для
// статичной картинки больше не нужно), длительность по аудио (-shortest).
// Прогресс считается из -progress pipe:1 (out_time_us) против длительности,
// которую ffmpeg сам печатает в баннере stderr.
func RenderStill(ctx context.Context, ffmpeg, imagePath, audioPath, outPath string, opts RenderOpts, progress func(float64)) error {
	if opts.Overlay {
		if err := ValidateFont(opts.Font); err != nil {
			return err
		}
	}

	chain, cleanup := buildChain(opts, outPath)
	defer cleanup()

	args := []string{
		"-y", "-hide_banner",
		"-loop", "1", "-i", imagePath,
		"-i", audioPath,
		"-filter_complex", chain,
		"-map", "[v]", "-map", "1:a",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-r", "2",
		"-c:a", "aac", "-b:a", "256k", "-ar", "48000",
		"-shortest", "-movflags", "+faststart",
	}
	if opts.MaxSeconds > 0 {
		args = append(args, "-t", strconv.FormatFloat(opts.MaxSeconds, 'f', 2, 64))
	}
	args = append(args, "-progress", "pipe:1", "-nostats", outPath)

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	hideWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// stderr: вылавливаем длительность аудио и держим хвост для сообщения об
	// ошибке (сам прогресс идёт по stdout).
	var (
		mu       sync.Mutex
		totalSec float64
		tail     []string
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			if totalSec == 0 {
				if m := durRe.FindStringSubmatch(line); m != nil {
					h, _ := strconv.ParseFloat(m[1], 64)
					mn, _ := strconv.ParseFloat(m[2], 64)
					s, _ := strconv.ParseFloat(m[3], 64)
					totalSec = h*3600 + mn*60 + s
				}
			}
			tail = append(tail, line)
			if len(tail) > 30 {
				tail = tail[1:]
			}
			mu.Unlock()
		}
	}()

	// stdout: -progress пишет блоки key=value; out_time_us — микросекунды.
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			var us float64
			if v, ok := strings.CutPrefix(line, "out_time_us="); ok {
				us, _ = strconv.ParseFloat(v, 64)
			} else if v, ok := strings.CutPrefix(line, "out_time_ms="); ok {
				// Историческая причуда ffmpeg: out_time_ms — тоже микросекунды.
				us, _ = strconv.ParseFloat(v, 64)
			} else {
				continue
			}
			mu.Lock()
			total := totalSec
			mu.Unlock()
			if progress != nil && total > 0 && us > 0 {
				p := (us / 1e6) / total
				if p > 1 {
					p = 1
				}
				progress(p)
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	err = cmd.Wait()
	wg.Wait()
	if err != nil {
		mu.Lock()
		msg := strings.Join(tail, "\n")
		mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, msg)
	}
	return nil
}
