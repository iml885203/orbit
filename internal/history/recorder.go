package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const ringLimit = 500
const rotateAtBytes = 5 * 1024 * 1024

type Recorder struct {
	mu   sync.Mutex
	path string
	file *os.File
	ring []Record
	subs map[chan Record]struct{}
}

func New(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	r := &Recorder{path: filepath.Join(dir, "history.jsonl"), subs: map[chan Record]struct{}{}}
	r.load()
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Recorder) Record(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.ID == "" {
		rec.ID = NewID()
	}
	r.appendRingLocked(rec)
	_ = r.writeLocked(rec)
	for ch := range r.subs {
		select {
		case ch <- rec:
		default:
			close(ch)
			delete(r.subs, ch)
		}
	}
}

// Subscribe returns a channel that receives every Record. On subscribe the
// channel is pre-loaded with the current ring (the last ringLimit records);
// new records arrive on Record() calls. Caller MUST invoke cancel when done.
//
// Drop policy: records are incremental, not snapshots, so coalescing would
// lose data. Buffer is sized ringLimit+32 (532) for the initial replay plus
// headroom. An extremely stalled subscriber that overflows is dropped to keep
// the daemon healthy; realistic SSE pipes drain well within the buffer.
func (r *Recorder) Subscribe() (<-chan Record, func()) {
	ch := make(chan Record, ringLimit+32)
	r.mu.Lock()
	for i := range r.ring {
		ch <- r.ring[i]
	}
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) List(f Filter) []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, 0, len(r.ring))
	seen := make(map[string]struct{}, len(r.ring))
	for i := len(r.ring) - 1; i >= 0; i-- {
		rec := r.ring[i]
		if rec.ID != "" {
			if _, ok := seen[rec.ID]; ok {
				continue
			}
			seen[rec.ID] = struct{}{}
		}
		if f.Source != "" && rec.Source != f.Source {
			continue
		}
		if f.OnlyNoCLI && rec.HasCLI {
			continue
		}
		if f.OnlyErrors && rec.Status != StatusError {
			continue
		}
		out = append(out, rec)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subs {
		close(ch)
		delete(r.subs, ch)
	}
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

func (r *Recorder) load() {
	f, err := os.Open(r.path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	var recs []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Record
		if json.Unmarshal(sc.Bytes(), &rec) == nil {
			recs = append(recs, rec)
		}
	}
	if len(recs) > ringLimit {
		recs = recs[len(recs)-ringLimit:]
	}
	r.ring = recs
}

func (r *Recorder) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	r.file = f
	return nil
}

func (r *Recorder) writeLocked(rec Record) error {
	if r.file == nil {
		if err := r.open(); err != nil {
			return err
		}
	}
	if st, err := r.file.Stat(); err == nil && st.Size() > rotateAtBytes {
		_ = r.file.Close()
		_ = os.Rename(r.path, r.path+".1")
		r.file = nil
		if err := r.open(); err != nil {
			return err
		}
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := r.file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (r *Recorder) appendRingLocked(rec Record) {
	r.ring = append(r.ring, rec)
	if len(r.ring) > ringLimit {
		copy(r.ring, r.ring[len(r.ring)-ringLimit:])
		r.ring = r.ring[:ringLimit]
	}
}
