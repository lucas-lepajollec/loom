package loom

import (
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
	"strings"
)

// Historical names and JSON remain intact while pure discussion logic lives in
// the leaf package. Loom owns storage, assembled context, selection and consent.
type Message = discussion.Message
type ToolCall = discussion.ToolCall
type ToolCallFunc = discussion.ToolCallFunc
type DiscussionEvent = discussion.DiscussionEvent
type ACPChangedFile = discussion.ACPChangedFile
type ACPState = discussion.ACPState
type NativeImport = discussion.NativeImport
type RuntimeSession = discussion.RuntimeSession[RuntimeUsage, StatsEvent]
type RuntimeTurnRecord = discussion.RuntimeTurnRecord[RuntimeUsage, StatsEvent]
type ContextItem = discussion.ContextItem
type DiscussionContext = discussion.DiscussionContext[Capability]
type DiscussionPreview = discussion.DiscussionPreview[Capability]

const maxDiscussionInstructions = discussion.MaxDiscussionInstructions
const maxPortableBytes = discussion.MaxPortableBytes
const maxPortableMessages = discussion.MaxPortableMessages
const maxMessageBytes = discussion.MaxMessageBytes

func cloneACPState(s ACPState) ACPState                   { return discussion.CloneACPState(s) }
func cloneRuntimeSession(s RuntimeSession) RuntimeSession { return discussion.CloneRuntimeSession(s) }
func prepareDiscussion(s RuntimeSession, draft string) DiscussionPreview {
	query := strings.TrimSpace(draft)
	if query == "" {
		if s.Status == "running" {
			query = lastUserText(portableMessages(s))
		} else {
			return discussion.PrepareDiscussion(s, draft, discussionContext(s))
		}
	}
	return discussion.PrepareDiscussion(s, draft, discussionContextFor(s, query))
}
func discussionContextRevision(s RuntimeSession, c DiscussionContext) string {
	return discussion.ContextRevision(s, c)
}
func portablePrefix(prefix, messages []Message) bool {
	return discussion.PortablePrefix(prefix, messages)
}
func discussionTitle(text string) string { return discussion.TitleFromText(text) }
func discussionTurnRecord(s RuntimeSession, p DiscussionPreview, index int) RuntimeTurnRecord {
	return discussion.NewTurnRecord(s, p, index)
}

type UsagePrice = discussion.UsagePrice
type ModelUsageSummary = discussion.ModelUsageSummary[RuntimeUsage]

func accumulateDiscussionUsage(rows map[string]*ModelUsageSummary, sessions []RuntimeSession) {
	discussion.AccumulateTurnUsage(rows, sessions, harnessRuntime, cloudChoiceID, discussion.UsageOps[RuntimeUsage]{
		Valid: func(u RuntimeUsage) bool { return u.Input >= 0 && u.Output >= 0 && u.Total >= 0 },
		Add: func(total *RuntimeUsage, u RuntimeUsage) {
			total.Input += u.Input
			total.Output += u.Output
			total.Total += u.Total
			total.Thinking += u.Thinking
			total.Cached += u.Cached
		},
	})
}
func accumulateDiscussionCosts(rows map[string]*ModelUsageSummary, sessions []RuntimeSession) {
	discussion.AccumulateHarnessCost(rows, sessions, harnessRuntime)
}
func discussionUsageSummaries(rows map[string]*ModelUsageSummary) []ModelUsageSummary {
	return discussion.UsageSummaries(rows, harnessRuntime, func(u RuntimeUsage, p UsagePrice) float64 {
		return estimatedCloudCost(u.Input, u.Output, p.Input, p.Output)
	})
}

func nativeDiscussionEvents(d map[string]any) []DiscussionEvent {
	return discussion.NativeDiscussionEvents(d)
}
func runtimeDiscussionEvents(e StreamEvent, turn RuntimeTurnRecord) []DiscussionEvent {
	return discussion.RuntimeDiscussionEvents(discussion.StreamEvent[RuntimeUsage, StatsEvent]{Content: e.Content, Reasoning: e.Reasoning, Usage: e.Usage, Stats: e.Stats, HarnessEvent: e.HarnessEvent, ACPEvent: e.ACPEvent, ACPState: e.ACPState}, turn)
}

// Text/ACP deltas carry no full provenance snapshot. Only metadata events need
// a copied current turn, never the complete history on each streamed token.
func liveRuntimeDiscussionEvents(e StreamEvent, turn RuntimeTurnRecord) []DiscussionEvent {
	projection := RuntimeTurnRecord{RuntimeID: turn.RuntimeID}
	if e.ACPEvent == nil && e.ACPState == nil && (e.Usage != nil || e.Stats != nil || e.HarnessEvent != nil) {
		projection = discussion.CloneRuntimeTurn(turn)
	}
	return runtimeDiscussionEvents(e, projection)
}
func runtimeReplay(s RuntimeSession) []DiscussionEvent {
	return discussion.RuntimeReplay(s, func() any { return discussionContext(s) })
}
func discussionMetrics(turn *RuntimeTurnRecord) DiscussionEvent {
	return discussion.DiscussionMetrics(turn)
}
