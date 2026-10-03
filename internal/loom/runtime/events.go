package runtime

// EventSink applies backpressure synchronously. Returning false asks the adapter
// to stop emitting and cancel its upstream work. The application supplies the
// event payload and projects it once into its persisted discussion journal;
// transient transport controls and native state are never portable messages.
type EventSink[Event any] func(Event) bool

// HarnessEvent is a portable display fact about a native tool, independent of
// the harness that reported it. JSON remains compatible with historical turns.
type HarnessEvent struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Failed     bool   `json:"failed,omitempty"`
	FileTarget string `json:"file_target,omitempty"`
}
