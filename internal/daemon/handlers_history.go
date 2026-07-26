package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/iml885203/orbit/internal/cmdmap/gaps"
	"github.com/iml885203/orbit/internal/history"
)

func registerHistoryHandlers(mux *http.ServeMux, rec *history.Recorder, gc *gaps.Collector) {
	mux.HandleFunc("/api/history/list", func(w http.ResponseWriter, r *http.Request) {
		handleHistoryList(w, r, rec)
	})
	mux.HandleFunc("/api/history/gaps", func(w http.ResponseWriter, r *http.Request) {
		handleHistoryGaps(w, r, gc)
	})
	mux.HandleFunc("/api/history/cli-event", func(w http.ResponseWriter, r *http.Request) {
		handleHistoryCLIEvent(w, r, rec)
	})
}

func handleHistoryList(w http.ResponseWriter, r *http.Request, rec *history.Recorder) {
	if requireMethod(w, r, http.MethodGet) || rec == nil {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	filter := history.Filter{
		Source:     history.Source(q.Get("source")),
		OnlyNoCLI:  q.Get("onlyNoCli") == "true",
		OnlyErrors: q.Get("onlyErrors") == "true",
		Limit:      limit,
	}
	writeJSON(w, http.StatusOK, rec.List(filter))
}

func handleHistoryGaps(w http.ResponseWriter, r *http.Request, gc *gaps.Collector) {
	if requireMethod(w, r, http.MethodGet) || gc == nil {
		return
	}
	writeJSON(w, http.StatusOK, gc.List())
}

func handleHistoryCLIEvent(w http.ResponseWriter, r *http.Request, rec *history.Recorder) {
	if requireMethod(w, r, http.MethodPost) || rec == nil {
		return
	}
	var in history.Record
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}
	if in.Source == "" {
		in.Source = history.SourceCLI
	}
	if in.Timestamp.IsZero() {
		in.Timestamp = time.Now()
	}
	rec.Record(in)
	writeJSON(w, http.StatusOK, APIResponse{OK: true})
}
