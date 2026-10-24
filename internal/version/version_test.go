package version

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	base := `^cruworldwine-go/` + regexp.QuoteMeta(Version) + ` go/[0-9][^ ()]* \(` +
		regexp.QuoteMeta(runtime.GOOS) + `; ` + regexp.QuoteMeta(runtime.GOARCH) + `\)`
	if ua := UserAgent(""); !regexp.MustCompile(base + `$`).MatchString(ua) {
		t.Errorf("UserAgent(\"\") = %q", ua)
	}
	if ua := UserAgent("cellar/1"); !regexp.MustCompile(base + ` cellar/1$`).MatchString(ua) {
		t.Errorf("UserAgent(app) = %q", ua)
	}
	if v := goVersion(); v == "" || strings.HasPrefix(v, "go") || strings.Contains(v, " ") {
		t.Errorf("goVersion() = %q", v)
	}
}

func TestAppInfo(t *testing.T) {
	for _, tc := range []struct{ name, version, url, want string }{
		{"Cellar Sync", "2.1.0", "https://example.test/cellar", "Cellar Sync/2.1.0 (https://example.test/cellar)"},
		{"cellar", "", "", "cellar"},
		{"cellar", "", "https://example.test", "cellar (https://example.test)"},
		{"", "1.0", "https://example.test", ""},
		{" \t", "1.0", "", ""},
		{"evil\r\nX-Injected: 1", "1\n", "", "evilX-Injected: 1/1"},
		{"del\x7f", "", "", "del"},
	} {
		if got := AppInfo(tc.name, tc.version, tc.url); got != tc.want {
			t.Errorf("AppInfo(%q, %q, %q) = %q, want %q", tc.name, tc.version, tc.url, got, tc.want)
		}
	}
}
