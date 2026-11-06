package urnettools

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mesoReleasePrefixes are the only URL prefixes the update path may build.
// The auto-update timer runs `urnet-tools update -y`; if any of these
// pointed at another fork, the timer would replace the meso provider and
// tool with that fork's build.
const (
	mesoAPIPrefix      = "https://api.github.com/repos/urfoundation/meso-miner"
	mesoDownloadPrefix = "https://github.com/urfoundation/meso-miner/releases/download/"
)

func TestReleaseURLBuildersUseMesoRepo(t *testing.T) {
	tag := "v2026.10.20-1076956260-meso"
	if repoAPIBase != mesoAPIPrefix {
		t.Errorf("repoAPIBase = %q, want %q", repoAPIBase, mesoAPIPrefix)
	}
	cases := map[string]string{
		"providerTarballURL": providerTarballURL(tag),
		"toolAssetURL":       toolAssetURL(tag, "urnet-tools-linux-amd64"),
	}
	want := map[string]string{
		"providerTarballURL": mesoDownloadPrefix + tag + "/urnetwork-provider-" + tag + ".tar.gz",
		"toolAssetURL":       mesoDownloadPrefix + tag + "/urnet-tools-linux-amd64",
	}
	for name, got := range cases {
		if got != want[name] {
			t.Errorf("%s = %q, want %q", name, got, want[name])
		}
	}
}

// recordingTransport answers every request with a canned release JSON and
// records the requested URLs, so the fetch functions can be exercised
// without the network.
type recordingTransport struct {
	mu   sync.Mutex
	urls []string
	body string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.urls = append(rt.urls, req.URL.String())
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Request:    req,
	}, nil
}

func withRecordingTransport(t *testing.T, body string) *recordingTransport {
	t.Helper()
	rt := &recordingTransport{body: body}
	prev := http.DefaultTransport
	http.DefaultTransport = rt
	latestReleaseMu.Lock()
	prevInfo, prevTime := cachedLatest, cachedLatestTime
	cachedLatest, cachedLatestTime = nil, time.Time{}
	latestReleaseMu.Unlock()
	t.Cleanup(func() {
		http.DefaultTransport = prev
		latestReleaseMu.Lock()
		cachedLatest, cachedLatestTime = prevInfo, prevTime
		latestReleaseMu.Unlock()
	})
	return rt
}

func TestFetchLatestReleaseUsesMesoRepo(t *testing.T) {
	tag := "v2026.10.20-1076956260-meso"
	// fetchLatestRelease walks the releases listing, so the canned body is a
	// JSON array (one page) rather than a single release object.
	rt := withRecordingTransport(t, `[{"tag_name":"`+tag+`","assets":[{"name":"urnetwork-provider-`+tag+`.tar.gz","digest":"sha256:abc"}]}]`)
	info, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	wantURL := mesoAPIPrefix + "/releases?per_page=100&page=1"
	if len(rt.urls) != 1 || rt.urls[0] != wantURL {
		t.Errorf("requested %v, want [%s]", rt.urls, wantURL)
	}
	if !strings.HasPrefix(info.URL, mesoDownloadPrefix) {
		t.Errorf("download URL %q is not under %s", info.URL, mesoDownloadPrefix)
	}
}

func TestFetchReleaseByTagUsesMesoRepo(t *testing.T) {
	tag := "v2026.10.20-1076956260-meso"
	rt := withRecordingTransport(t, `{"tag_name":"`+tag+`","assets":[{"name":"urnetwork-provider-`+tag+`.tar.gz","digest":"sha256:abc"}]}`)
	info, err := fetchReleaseByTag(tag)
	if err != nil {
		t.Fatalf("fetchReleaseByTag: %v", err)
	}
	wantURL := mesoAPIPrefix + "/releases/tags/" + tag
	if len(rt.urls) != 1 || rt.urls[0] != wantURL {
		t.Errorf("requested %v, want [%s]", rt.urls, wantURL)
	}
	if !strings.HasPrefix(info.URL, mesoDownloadPrefix) {
		t.Errorf("download URL %q is not under %s", info.URL, mesoDownloadPrefix)
	}
}

// TestNoForeignReleaseSlugInSource guards against a hardcoded release URL
// for another fork creeping back into the tool's non-test sources.
func TestNoForeignReleaseSlugInSource(t *testing.T) {
	// Go sources, the installer scripts AND the workflows: the FreeBSD
	// installer was once copied verbatim from the sibling fork, so it fetched
	// that fork's tarball, tool asset and shell-wrapper, cross-contaminating
	// every FreeBSD install in this repo. Checking only *.go could not see it.
	patterns := []string{
		"*.go",
		"../../scripts/*.sh",
		"../../scripts/*.ps1",
		"../../.github/workflows/*.yml",
	}
	var files []string
	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no files matched; the glob is wrong and this test would pass vacuously")
	}
	// This repo must only fetch its own releases. The fork this tree was synced
	// from (full-bars/meso-miner, and its older urnetwork-3.23-fix name) is a
	// foreign slug here: a shipped artifact that still names it would
	// auto-update the provider from that fork. A legitimate mention of another
	// sibling repo (full-bars/connect, which the drift monitor tracks) is not
	// the target.
	foreignSlugs := []string{"full-bars/meso-miner", "full-bars/urnetwork-3.23-fix"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, slug := range foreignSlugs {
			if strings.Contains(string(b), slug) {
				t.Errorf("%s references the foreign release slug %s; this repo must only fetch urfoundation/meso-miner", f, slug)
			}
		}
	}
}
