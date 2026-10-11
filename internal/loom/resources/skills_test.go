package resources

import (
	"testing"
)

func TestSkillFrontMatterParsing(t *testing.T) {
	meta, body := ParseSkillMarkdown("\uFEFF---\nname: x\ndescription: 'a: b'\nmetadata:\n  title: Titre\n  loom-id: 7\n---\n\nCorps\n")
	if meta["name"] != "x" || meta["description"] != "a: b" || meta["metadata.title"] != "Titre" || meta["metadata.loom-id"] != "7" || body != "Corps" {
		t.Fatalf("%v %q", meta, body)
	}
	if _, body := ParseSkillMarkdown("Sans en-tête"); body != "Sans en-tête" {
		t.Fatal(body)
	}
}
