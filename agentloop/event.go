package agentloop

// Event types reported through Callbacks.OnEvent.
const (
	// EventAssistantText carries the model's final answer.
	EventAssistantText = "assistant_text"
	// EventToolStart is emitted before a tool runs.
	EventToolStart = "tool_start"
	// EventToolDone carries the payload the tool returned to the model.
	EventToolDone = "tool_done"
	// EventReport carries the execution report of the whole run.
	EventReport = "report"
	// EventWarning carries a condition worth showing that did not stop the run.
	EventWarning = "warning"
	// EventError carries the failure that ended the run.
	EventError = "error"
	// EventDone is the last event of a successful run.
	EventDone = "done"
)

// Event is one milestone of a run. Which fields are set depends on Type.
type Event struct {
	Type string

	// Text holds the assistant's answer for EventAssistantText.
	Text string
	// Err holds the failure for EventError.
	Err error
	// Report holds the execution report for EventReport.
	Report string
	// Message holds human-readable text for EventWarning and EventError.
	Message string

	// ToolName, ToolArgs and ToolResult describe a tool call. ToolResult is
	// set on EventToolDone only.
	ToolName   string
	ToolArgs   string
	ToolResult string
	ToolError  bool
}

// Callbacks observes a run. Every field is optional.
//
// OnStatus reports progress that is not part of the transcript — waiting for
// the model, retrying, running a tool — so a UI can show a line that changes
// in place instead of appending to the conversation.
type Callbacks struct {
	OnEvent  func(Event)
	OnStatus func(phase, message string)
}

func (cb Callbacks) emit(ev Event) {
	if cb.OnEvent != nil {
		cb.OnEvent(ev)
	}
}

func (cb Callbacks) status(phase, message string) {
	if cb.OnStatus != nil {
		cb.OnStatus(phase, message)
	}
}
