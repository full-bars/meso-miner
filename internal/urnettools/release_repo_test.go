package urnettools

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// mesoReleasePrefixes are the only URL prefixes the update path may build.
// The auto-update timer runs `urnet-tools update -y`; if any of these
// pointed at another fork, the timer would replace the meso provider and
// tool with that fork's build.
const (
	mesoAPIPrefix      = "https://api.github.com/repos/full-bars/meso-miner/releases/"
	mesoDownloadPrefix = "https://github.com/full-bars/meso-miner/releases/download/"
)

func TestReleaseURLBuildersUseMesoRepo(t *testing.T) {
	tag := "v2026.9.22-1052862940-meso"
	cases := map[string]string{
		"releaseLatestAPIURL": releaseLatestAPIURL(),
		"releaseTagAPIURL":    releaseTagAPIURL(tag),
		"providerTarballURL":  providerTarballURL(tag),
		"toolAssetURL":        toolAssetURL(tag, "urnet-tools-linux-amd64"),
	}
	want := map[string]string{
		"releaseLatestAPIURL": mesoAPIPrefix + "latest",
		"releaseTagAPIURL":    mesoAPIPrefix + "tags/" + tag,
		"providerTarballURL":  mesoDownloadPrefix + tag + "/urnetwork-provider-" + tag + ".tar.gz",
		"toolAssetURL":        mesoDownloadPrefix + tag + "/urnet-tools-linux-amd64",
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
	t.Cleanup(func() { http.DefaultTransport = prev })
	return rt
}

func TestFetchLatestReleaseUsesMesoRepo(t *testing.T) {
	tag := "v2026.9.22-1052862940-meso"
	rt := withRecordingTransport(t, `{"tag_name":"`+tag+`","assets":[{"name":"urnetwork-provider-`+tag+`.tar.gz","digest":"sha256:abc"}]}`)
	info, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if len(rt.urls) != 1 || rt.urls[0] != mesoAPIPrefix+"latest" {
		t.Errorf("requested %v, want [%s]", rt.urls, mesoAPIPrefix+"latest")
	}
	if !strings.HasPrefix(info.URL, mesoDownloadPrefix) {
		t.Errorf("download URL %q is not under %s", info.URL, mesoDownloadPrefix)
	}
}

func TestFetchReleaseByTagUsesMesoRepo(t *testing.T) {
	tag := "v2026.9.22-1052862940-meso"
	rt := withRecordingTransport(t, `{"tag_name":"`+tag+`","assets":[{"name":"urnetwork-provider-`+tag+`.tar.gz","digest":"sha256:abc"}]}`)
	info, err := fetchReleaseByTag(tag)
	if err != nil {
		t.Fatalf("fetchReleaseByTag: %v", err)
	}
	if len(rt.urls) != 1 || rt.urls[0] != mesoAPIPrefix+"tags/"+tag {
		t.Errorf("requested %v, want [%s]", rt.urls, mesoAPIPrefix+"tags/"+tag)
	}
	if !strings.HasPrefix(info.URL, mesoDownloadPrefix) {
		t.Errorf("download URL %q is not under %s", info.URL, mesoDownloadPrefix)
	}
}

// TestNoForeignReleaseSlugInSource guards against a hardcoded release URL
// for another fork creeping back into the tool's non-test sources.
func TestNoForeignReleaseSlugInSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "full-bars/urnetwork-3.23-fix") {
			t.Errorf("%s references full-bars/urnetwork-3.23-fix; use releaseRepo", f)
		}
	}
}
