package ghupdate

// Fork (mattzab/beszel): the hub compares fork releases against ForkVersion.

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/blang/semver"
)

type fakeReleaseClient struct {
	tag string
	url string
}

func (c *fakeReleaseClient) Do(req *http.Request) (*http.Response, error) {
	c.url = req.URL.String()
	body := `{"tag_name":"` + c.tag + `","assets":[]}`
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestForkVersionOrdering(t *testing.T) {
	order := []string{"0.21.1-mz.1", "0.21.1-mz.2", "0.21.1-mz.10", "0.21.2-mz.1", "0.22.0-mz.1"}
	for i := 1; i < len(order); i++ {
		a, b := semver.MustParse(order[i-1]), semver.MustParse(order[i])
		if !b.GT(a) {
			t.Errorf("%s should be newer than %s", order[i], order[i-1])
		}
	}
}

func TestUpdateUsesCurrentVersionAndOwner(t *testing.T) {
	client := &fakeReleaseClient{tag: "v0.21.1-mz.1"}
	updated, err := Update(Config{
		ArchiveExecutable: "beszel",
		DataDir:           t.TempDir(),
		HttpClient:        client,
		Owner:             "mattzab",
		CurrentVersion:    "0.21.1-mz.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated {
		t.Fatal("same fork version must not update")
	}
	if client.url != "https://api.github.com/repos/mattzab/beszel/releases/latest" {
		t.Fatalf("unexpected release URL %q", client.url)
	}
}
