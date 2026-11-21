package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

type brainDistilledSource struct {
	DiscussionID string `json:"discussion_id"`
	MessageIndex int    `json:"message_index"`
}
type brainDistilledItem struct {
	Review string               `json:"review,omitempty"`
	ID     string               `json:"id"`
	Kind   string               `json:"kind"`
	Text   string               `json:"text"`
	Source brainDistilledSource `json:"source"`
	Date   time.Time            `json:"date"`
}

func (s *brainService) loadDistilledLocked() ([]brainDistilledItem, error) {
	if err := brainAvailable(); err != nil {
		return nil, err
	}
	b, err := s.storage.read("distilled.json", 4<<20)
	if err != nil {
		return nil, err
	}
	out := []brainDistilledItem{}
	if len(b) == 0 {
		return out, nil
	}
	b, err = decodeMemContent(b)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}
func (s *brainService) distilledDocuments(ctx context.Context, emit func(brain.Document) bool) error {
	items, err := s.loadDistilledLocked()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Review != "" && item.Review != "accepted" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		text := fmt.Sprintf("# %s\n\n%s\n\nSource: discussion %s, message %d\nDate: %s", item.Kind, item.Text, item.Source.DiscussionID, item.Source.MessageIndex, item.Date.Format(time.RFC3339))
		if !emit(brain.Document{Path: item.ID + ".md", Text: text}) {
			break
		}
	}
	return nil
}
