package discussion

func PortablePrefix(prefix, messages []Message) bool {
	if len(prefix) > len(messages) {
		return false
	}
	for i, msg := range prefix {
		text, ok := msg.Content.(string)
		other, otherOK := messages[i].Content.(string)
		if !ok || !otherOK || msg.Role != messages[i].Role || text != other {
			return false
		}
	}
	return true
}

// TitleFromText keeps the historical 70-rune automatic discussion title.
func TitleFromText(text string) string {
	title := []rune(text)
	if len(title) > 70 {
		title = title[:70]
	}
	return string(title)
}

// NewTurnRecord captures resolved route and prepared-input provenance only.
func NewTurnRecord[Usage, Stats, Capability any](s RuntimeSession[Usage, Stats], p DiscussionPreview[Capability], messageIndex int) RuntimeTurnRecord[Usage, Stats] {
	return RuntimeTurnRecord[Usage, Stats]{MessageIndex: messageIndex, RuntimeID: s.RuntimeID, ProviderName: s.ProviderName, ProviderID: s.ProviderID, Endpoint: s.Endpoint, Model: s.Model, ContextBytes: len(p.Context.System), ContextRevision: p.Context.Revision, InputBytes: p.TextBytes}
}
