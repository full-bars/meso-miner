package urnettools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForeignImageNameInWorkflows guards the publish path: no workflow
// may carry a fix-line image name or Docker Hub publish machinery again.
// The sibling guard (TestNoForeignReleaseSlugInSource) checks the bare repo
// slug, but the fix line's Docker Hub image does not contain that slug, so
// until 2026-10-17 build.yml was configured to push it while that guard was
// green. meso publishes exactly one image: ghcr.io/${{ github.repository }}
// (ghcr.io/full-bars/meso-miner). If a second image target is ever actually
// wanted, change this test deliberately.
func TestNoForeignImageNameInWorkflows(t *testing.T) {
	forbidden := []string{
		"urnetwork-3.23-fix", // any registry spelling of the fix line's image
		"3cape/",             // the fix line's Docker Hub org
		"DOCKER_IMAGE_NAME",  // the second-image env var removed from build.yml
		"DOCKERHUB",          // Docker Hub credential path
		"docker.io",          // explicit Docker Hub registry spelling
	}

	var files []string
	for _, pat := range []string{"../../.github/workflows/*.yml", "../../.github/workflows/*.yaml"} {
		matches, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no workflow files matched; the glob is wrong and this test would pass vacuously")
	}

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, pat := range forbidden {
			if strings.Contains(string(b), pat) {
				t.Errorf("%s contains %q; meso publishes only ghcr.io/full-bars/meso-miner", f, pat)
			}
		}
	}
}
