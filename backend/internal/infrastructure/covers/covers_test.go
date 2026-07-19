package covers

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// pinterestStub emulates the two Pinterest endpoints covers.Search relies on:
// the homepage (which seeds the csrftoken cookie) and the BaseSearchResource
// endpoint (which returns pins). resourceHandler lets each test shape the
// search response and inspect the incoming request.
type pinterestStub struct {
	srv             *httptest.Server
	csrftoken       string
	resourceHandler func(w http.ResponseWriter, r *http.Request)
}

func newPinterestStub(t *testing.T, resource func(w http.ResponseWriter, r *http.Request)) *pinterestStub {
	t.Helper()
	st := &pinterestStub{csrftoken: "tok-123", resourceHandler: resource}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: st.csrftoken, Path: "/"})
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/resource/BaseSearchResource/get/", st.resourceHandler)
	st.srv = httptest.NewServer(mux)

	prev := pinterestBase
	pinterestBase = st.srv.URL
	t.Cleanup(func() {
		pinterestBase = prev
		st.srv.Close()
	})
	return st
}

// newJarClient returns an http.Client with a cookie jar, как в NewCoverService.
func newJarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

const pageOneTwoResults = `{"resource_response":{"bookmark":"","data":{"results":[
	{"id":"1","images":{"orig":{"url":"https://i.pinimg.com/originals/aa.jpg","width":736,"height":900},"236x":{"url":"https://i.pinimg.com/236x/aa.jpg"}}},
	{"id":"2","images":{"236x":{"url":"https://i.pinimg.com/236x/bb.jpg"}}}
]}}}`

func TestSearchMapsPinterestResults(t *testing.T) {
	newPinterestStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(pageOneTwoResults))
	})

	imgs, err := Search(context.Background(), newJarClient(t), "dark aesthetic", 40)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1 image (result without orig skipped), got %d: %+v", len(imgs), imgs)
	}
	got := imgs[0]
	if got.ID != "1" {
		t.Errorf("ID: want 1, got %q", got.ID)
	}
	if got.Full != "https://i.pinimg.com/originals/aa.jpg" {
		t.Errorf("Full: got %q", got.Full)
	}
	if got.Thumb != "https://i.pinimg.com/236x/aa.jpg" {
		t.Errorf("Thumb: got %q", got.Thumb)
	}
	if got.Width != 736 || got.Height != 900 {
		t.Errorf("size: got %dx%d", got.Width, got.Height)
	}
}

func TestSearchSendsCSRFTokenFromCookie(t *testing.T) {
	var gotToken string
	newPinterestStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-CSRFToken")
		w.Write([]byte(pageOneTwoResults))
	})

	if _, err := Search(context.Background(), newJarClient(t), "q", 40); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotToken != "tok-123" {
		t.Errorf("X-CSRFToken: want cookie value tok-123, got %q", gotToken)
	}
}

func TestSearchPaginatesAndDedups(t *testing.T) {
	page1 := `{"resource_response":{"bookmark":"BM2","data":{"results":[
		{"id":"1","images":{"orig":{"url":"https://i.pinimg.com/originals/a.jpg"}}},
		{"id":"2","images":{"orig":{"url":"https://i.pinimg.com/originals/b.jpg"}}}
	]}}}`
	// page2 repeats b.jpg (must be deduped) and adds c.jpg, then bookmark empties.
	page2 := `{"resource_response":{"bookmark":"","data":{"results":[
		{"id":"2","images":{"orig":{"url":"https://i.pinimg.com/originals/b.jpg"}}},
		{"id":"3","images":{"orig":{"url":"https://i.pinimg.com/originals/c.jpg"}}}
	]}}}`

	newPinterestStub(t, func(w http.ResponseWriter, r *http.Request) {
		data := r.URL.Query().Get("data")
		if strings.Contains(data, "BM2") {
			w.Write([]byte(page2))
			return
		}
		w.Write([]byte(page1))
	})

	imgs, err := Search(context.Background(), newJarClient(t), "q", 40)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var urls []string
	for _, im := range imgs {
		urls = append(urls, im.Full)
	}
	want := "https://i.pinimg.com/originals/a.jpg,https://i.pinimg.com/originals/b.jpg,https://i.pinimg.com/originals/c.jpg"
	if strings.Join(urls, ",") != want {
		t.Errorf("paged/deduped urls:\n got %v\nwant %v", urls, want)
	}
}

func TestSearchTrimsToLimit(t *testing.T) {
	newPinterestStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(pageOneTwoResults)) // 1 usable image per page, bookmark ""
	})
	imgs, err := Search(context.Background(), newJarClient(t), "q", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("limit=1: want 1, got %d", len(imgs))
	}
}

func TestSearchReBootstrapsOn403(t *testing.T) {
	var calls int
	newPinterestStub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(pageOneTwoResults))
	})

	imgs, err := Search(context.Background(), newJarClient(t), "q", 40)
	if err != nil {
		t.Fatalf("Search after re-bootstrap: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1 image after retry, got %d", len(imgs))
	}
	if calls < 2 {
		t.Errorf("expected a retry after 403, resource called %d time(s)", calls)
	}
}

// sanity: variantQuery-style base override must produce a well-formed search URL.
func TestPinterestBaseIsOverridable(t *testing.T) {
	if _, err := url.Parse(pinterestBase); err != nil {
		t.Fatalf("pinterestBase not a URL: %v", err)
	}
}
