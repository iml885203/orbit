package cli

import "sync"

var jsonNotices struct {
	sync.Mutex
	pending []JSONNotice
}

// AddJSONNotice records transition metadata for the one Cobra command executed
// by an Orbit process. Execute() is synchronous and renders exactly one final
// envelope; takeJSONNotices drains the state so in-process tests cannot leak a
// notice into the next command.
func AddJSONNotice(notice JSONNotice) {
	jsonNotices.Lock()
	defer jsonNotices.Unlock()
	jsonNotices.pending = append(jsonNotices.pending, notice)
}

func takeJSONNotices() []JSONNotice {
	jsonNotices.Lock()
	defer jsonNotices.Unlock()
	notices := append([]JSONNotice(nil), jsonNotices.pending...)
	jsonNotices.pending = nil
	return notices
}
