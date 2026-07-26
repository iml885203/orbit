package history

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

var idMu sync.Mutex
var lastMs int64
var lastSeq uint16

// NewID returns a compact sortable-ish unique ID without adding a dependency.
func NewID() string {
	idMu.Lock()
	defer idMu.Unlock()
	ms := time.Now().UnixMilli()
	if ms == lastMs {
		lastSeq++
	} else {
		lastMs = ms
		lastSeq = 0
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return time.UnixMilli(ms).UTC().Format("20060102T150405.000") + "-" + hex.EncodeToString(b[:]) + "-" + hex.EncodeToString([]byte{byte(lastSeq >> 8), byte(lastSeq)})
}
