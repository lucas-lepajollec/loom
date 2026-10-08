package brain

import "strings"

const ProfileTag, ProjectNotesTag = "user-profile", "project-notes"
const ProfileLimit, ProjectNotesLimit = 1400, 2200

func ContextQuery(query string) string {
	query = strings.TrimSpace(query)
	if len(query) > 2000 {
		query = query[len(query)-2000:]
		for len(query) > 0 && query[0]&0xc0 == 0x80 {
			query = query[1:]
		}
	}
	return query
}
