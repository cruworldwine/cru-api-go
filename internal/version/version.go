// Package version holds the SDK's version and builds the User-Agent header it sends.
//
// The release workflow reads Version from this file and refuses a tag that does not match it.
package version

import (
	"runtime"
	"strings"
)

// Version is the SDK's version, without the leading v of its tag.
const Version = "0.2.0"

// UserAgent returns "cruworldwine-go/<Version> go/<Go version> (<GOOS>; <GOARCH>)", followed
// by app when it is not empty.
func UserAgent(app string) string {
	ua := "cruworldwine-go/" + Version + " go/" + goVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
	if app != "" {
		ua += " " + app
	}
	return ua
}

// AppInfo formats an application for the User-Agent as "name/version (url)", dropping the
// characters a header cannot carry. It is empty when name is.
func AppInfo(name, version, url string) string {
	name, version, url = headerText(name), headerText(version), headerText(url)
	if name == "" {
		return ""
	}
	out := name
	if version != "" {
		out += "/" + version
	}
	if url != "" {
		out += " (" + url + ")"
	}
	return out
}

// goVersion is runtime.Version() without its "go" prefix and anything after a space, such as
// "1.23.4"; a development toolchain is "devel".
func goVersion() string {
	v, _, _ := strings.Cut(runtime.Version(), " ")
	if strings.HasPrefix(v, "devel") {
		return "devel"
	}
	return strings.TrimPrefix(v, "go")
}

func headerText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
}
