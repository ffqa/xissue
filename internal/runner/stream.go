package runner

import "time"

const (
	streamFlushDelay = 150 * time.Millisecond
	streamFlushBytes = 8 * 1024
)

// queueStreamEvent coalesces high-frequency agent message/stderr chunks into
// one database row per short window. This keeps a chatty agent from writing
// thousands of tiny rows while preserving the visual stream.
func (r *Runner) queueStreamEvent(act *activeRun, typ string, data map[string]any) {
	text, _ := data["text"].(string)
	if text == "" {
		return
	}
	act.streamMu.Lock()
	if act.streams == nil {
		act.streams = map[string]*streamBuffer{}
	}
	buf := act.streams[typ]
	if buf == nil {
		buf = &streamBuffer{data: cloneEventData(data)}
		act.streams[typ] = buf
	}
	buf.text.WriteString(text)
	var payload map[string]any
	if buf.text.Len() >= streamFlushBytes {
		payload = cloneEventData(buf.data)
		payload["text"] = buf.text.String()
		delete(act.streams, typ)
		if len(act.streams) == 0 && act.streamTimer != nil {
			act.streamTimer.Stop()
			act.streamTimer = nil
		}
	} else if act.streamTimer == nil {
		act.streamTimer = time.AfterFunc(streamFlushDelay, func() { r.flushStreams(act) })
	}
	act.streamMu.Unlock()
	if payload != nil {
		r.emitEvent(act.runID, act.issueID, typ, payload)
	}
}

// flushStreams writes all pending coalesced chunks immediately. It is called
// before non-stream events and before a run reaches a terminal state.
func (r *Runner) flushStreams(act *activeRun) {
	act.streamMu.Lock()
	if act.streamTimer != nil {
		act.streamTimer.Stop()
		act.streamTimer = nil
	}
	pending := act.streams
	act.streams = nil
	if len(pending) == 0 {
		act.streamMu.Unlock()
		return
	}
	payloads := make(map[string]map[string]any, len(pending))
	for typ, buf := range pending {
		data := cloneEventData(buf.data)
		data["text"] = buf.text.String()
		payloads[typ] = data
	}
	act.streamMu.Unlock()
	for typ, data := range payloads {
		r.emitEvent(act.runID, act.issueID, typ, data)
	}
}

func cloneEventData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}
