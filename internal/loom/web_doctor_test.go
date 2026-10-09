package loom

import (
	"os"
	"regexp"
	"testing"
)

// Every "Fix" link must open a real interface page: the section must be a
// registered route (ui/next/js/app/routes.js).
func TestDoctorFixLinksOpenRealPages(t *testing.T) {
	src, err := os.ReadFile("web_doctor.go")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := os.ReadFile("ui/next/js/app/routes.js")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{ id: '([a-z]+)'`).FindAllStringSubmatch(string(routes), -1) {
		known[m[1]] = true
	}
	links := regexp.MustCompile(`"#/([a-z]+)[^"]*"`).FindAllStringSubmatch(string(src), -1)
	if len(links) == 0 {
		t.Fatal("no fix links found")
	}
	for _, l := range links {
		if !known[l[1]] {
			t.Errorf("fix link %s opens no page (known sections: %v)", l[0], known)
		}
	}
}
