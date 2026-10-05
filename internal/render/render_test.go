package render

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			shade := uint8((x + y) % 2 * 255)
			img.Set(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func imageServer(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()

	data := testPNG(t, 8, 8)
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if hits != nil {
				hits.Add(1)
			}
			if r.URL.Path == "/missing.png" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
		}))
	t.Cleanup(server.Close)

	return server
}

func useTempKittyDir(t *testing.T) {
	t.Helper()

	dir := kittyDir
	kittyDir = t.TempDir()
	t.Cleanup(func() { kittyDir = dir })
}

func TestRenderWithoutImages(t *testing.T) {
	renderer, err := New(Options{ImageMode: ImageModeNone, Width: 60})
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render("# Title\n\nSome body text.")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "Title") {
		t.Errorf("output does not contain the title:\n%s", out)
	}
	if !strings.Contains(out, "Some body text.") {
		t.Errorf("output does not contain the body:\n%s", out)
	}
}

func TestRenderKeepsImageMarkdownInNoneMode(t *testing.T) {
	server := imageServer(t, nil)

	renderer, err := New(Options{ImageMode: ImageModeNone, Width: 60})
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render("![Picture](" + server.URL + "/a.png)")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "rimg") {
		t.Errorf("output contains a placeholder:\n%s", out)
	}
	if !strings.Contains(out, "Picture") {
		t.Errorf("output does not contain the image title:\n%s", out)
	}
}

func TestRenderReplacesImages(t *testing.T) {
	server := imageServer(t, nil)
	useTempKittyDir(t)

	for _, mode := range []ImageMode{ImageModeANSI, ImageModeANSIDither,
		ImageModeKitty, ImageModeSixel} {
		t.Run(string(mode), func(t *testing.T) {
			renderer, err := New(Options{ImageMode: mode, Width: 40})
			if err != nil {
				t.Fatal(err)
			}

			out, err := renderer.Render("![Picture](" + server.URL + "/a.png)")
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if strings.Contains(out, "rimg") {
				t.Errorf("placeholder survived rendering:\n%q", out)
			}
			if !strings.Contains(out, "Picture") {
				t.Errorf("image title is missing:\n%q", out)
			}
			if !strings.Contains(out, "\x1b") && mode != ImageModeSixel {
				t.Errorf("no escape sequences in the output:\n%q", out)
			}
		})
	}
}

func TestRenderNarrowWidthKeepsPlaceholdersIntact(t *testing.T) {
	server := imageServer(t, nil)

	for _, width := range []int{5, 8, 20, 200} {
		renderer, err := New(Options{ImageMode: ImageModeANSI, Width: width})
		if err != nil {
			t.Fatal(err)
		}

		out, err := renderer.Render("A paragraph of text that wraps around " +
			"the terminal edge, followed by ![Picture](" + server.URL + "/a.png)")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if strings.Contains(out, "rimg") {
			t.Errorf("width %d broke the placeholder:\n%q", width, out)
		}
	}
}

func TestRenderFallsBackToTitle(t *testing.T) {
	server := imageServer(t, nil)

	renderer, err := New(Options{ImageMode: ImageModeANSI, Width: 60})
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render("![Picture](" + server.URL + "/missing.png)")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "rimg") {
		t.Errorf("placeholder survived rendering:\n%q", out)
	}
	if !strings.Contains(out, "Picture") {
		t.Errorf("output does not fall back to the image title:\n%q", out)
	}
}

func TestRenderFallsBackToURLWithoutTitle(t *testing.T) {
	renderer, err := New(Options{ImageMode: ImageModeANSI, Width: 60})
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render("![](gopher://example.com/a.png)")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "gopher://example.com/a.png") {
		t.Errorf("output does not fall back to the image URL:\n%q", out)
	}
}

func TestRenderDataURL(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(testPNG(t, 4, 4))

	renderer, err := New(Options{ImageMode: ImageModeANSI, Width: 40})
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render("![Inline](data:image/png;base64," + encoded + ")")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "\x1b") {
		t.Errorf("the inline image was not rendered:\n%q", out)
	}
}

func TestRenderFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, testPNG(t, 4, 4), 0o644); err != nil {
		t.Fatal(err)
	}

	renderer, err := New(Options{ImageMode: ImageModeANSI, Width: 40})
	if err != nil {
		t.Fatal(err)
	}

	urlPath := filepath.ToSlash(path)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}

	fileURL := (&url.URL{Scheme: "file", Path: urlPath}).String()

	out, err := renderer.Render("![Local](" + fileURL + ")")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "\x1b") {
		t.Errorf("the local image was not rendered:\n%q", out)
	}
}

func TestLoaderCachesByURL(t *testing.T) {
	var hits atomic.Int64
	server := imageServer(t, &hits)

	renderer, err := New(Options{ImageMode: ImageModeANSI, Width: 40})
	if err != nil {
		t.Fatal(err)
	}

	doc := "![One](" + server.URL + "/a.png)\n\n![Two](" + server.URL + "/a.png)"
	if _, err := renderer.Render(doc); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}

func TestNewDefaults(t *testing.T) {
	renderer, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if renderer.mode != ImageModeNone {
		t.Errorf("mode = %q, want %q", renderer.mode, ImageModeNone)
	}
	if renderer.width != DefaultWidth {
		t.Errorf("width = %d, want %d", renderer.width, DefaultWidth)
	}
}

func TestDecodeDataURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"base64", "data:text/plain;base64,aGVsbG8=", "hello"},
		{"uppercase scheme", "DATA:text/plain;base64,aGVsbG8=", "hello"},
		{"percent encoded", "data:text/plain,hello%20world", "hello world"},
		{"no media type", "data:,plain", "plain"},
		{"wrapped base64", "data:text/plain;base64,aGVs\nbG8=", "hello"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeDataURL(tc.url)
			if err != nil {
				t.Fatalf("decodeDataURL: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestDecodeDataURLMalformed(t *testing.T) {
	if _, err := decodeDataURL("data:text/plain;base64"); err == nil {
		t.Error("decoding a data URL without a comma succeeded")
	}
}

func TestIsDataURL(t *testing.T) {
	cases := map[string]bool{
		"data:image/png;base64,AAAA": true,
		"DaTa:image/png,AAAA":        true,
		"data":                       false,
		"https://example.com/a.png":  false,
		"":                           false,
	}

	for location, want := range cases {
		if got := isDataURL(location); got != want {
			t.Errorf("isDataURL(%q) = %t, want %t", location, got, want)
		}
	}
}

func TestDecodeImageRejectsGarbage(t *testing.T) {
	if _, err := decodeImage([]byte("not an image")); err == nil {
		t.Error("decoding garbage succeeded")
	}
}

func TestReadLimited(t *testing.T) {
	if _, err := readLimited(strings.NewReader("small")); err != nil {
		t.Errorf("readLimited: %v", err)
	}

	oversized := strings.NewReader(strings.Repeat("x", maxImageBytes+1))
	if _, err := readLimited(oversized); err == nil {
		t.Error("reading an oversized image succeeded")
	}
}

func TestEncodeImageUnsupportedMode(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))

	if _, err := encodeImage(img, ImageModeNone, 40); err == nil {
		t.Error("encoding in none mode succeeded")
	}
}

func TestEncodeKitty(t *testing.T) {
	useTempKittyDir(t)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))

	t.Setenv("TMUX", "")
	out, err := encodeKitty(img, 40)
	if err != nil {
		t.Fatalf("encodeKitty: %v", err)
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,U=1,q=2,f=100,t=f,i=") {
		t.Errorf("no virtual placement:\n%q", out)
	}
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("no placeholders:\n%q", out)
	}

	files, err := filepath.Glob(filepath.Join(kittyDir, "reader-*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("image files = %v (%v), want one", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("image file is not a PNG: %v", err)
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	out, err = encodeKitty(img, 40)
	if err != nil {
		t.Fatalf("encodeKitty: %v", err)
	}
	if !strings.HasPrefix(out, "\x1bPtmux;\x1b\x1b_G") {
		t.Errorf("no tmux passthrough:\n%q", out)
	}
}

func TestKittySize(t *testing.T) {
	cases := []struct {
		name               string
		width, height      int
		termWidth          int
		cellW, cellH       int
		wantCols, wantRows int
	}{
		{"natural size", 100, 100, 80, 10, 20, 10, 5},
		{"unknown cell size", 100, 100, 80, 0, 0, 10, 5},
		{"too wide", 2000, 400, 80, 10, 20, 66, 6},
		{"too tall", 100, 10000, 80, 10, 20, 5, 297},
		{"narrow terminal", 100, 100, 5, 10, 20, 1, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bounds := image.Rect(0, 0, tc.width, tc.height)
			cols, rows := kittySize(bounds, tc.termWidth, tc.cellW, tc.cellH)
			if cols != tc.wantCols || rows != tc.wantRows {
				t.Errorf("got %dx%d, want %dx%d",
					cols, rows, tc.wantCols, tc.wantRows)
			}
		})
	}
}

func TestDecorate(t *testing.T) {
	cases := []struct {
		encoded string
		title   string
		want    string
	}{
		{"IMG", "Title", "\nIMG\n  Title"},
		{"IMG", "", "\nIMG\n"},
		{"\nIMG\n", "Title", "\nIMG\n  Title"},
	}

	for _, tc := range cases {
		if got := decorate(tc.encoded, tc.title); got != tc.want {
			t.Errorf("decorate(%q, %q) = %q, want %q",
				tc.encoded, tc.title, got, tc.want)
		}
	}
}

func TestUnsupportedImageScheme(t *testing.T) {
	l := newLoader(0)

	if _, err := l.Load("gopher://example.com/a.png"); err == nil {
		t.Error("loading an unsupported scheme succeeded")
	}
}
