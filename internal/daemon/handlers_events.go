package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/iml885203/orbit/internal/engine"
)

// Event is one frame on the unified /api/events SSE stream. Type is sent as
// the SSE `event:` field so clients can dispatch via addEventListener; Data
// is JSON-encoded into `data:`.
//
// Drop policy varies by the shape of each source's data:
//
//   - status (snapshot, ticker-driven): observational. fanInStatus uses
//     non-blocking send and drops on a full out channel; the next 2s tick
//     catches up.
//   - log (incremental, high-volume): observational. fanInLogs uses
//     non-blocking send and drops on a full out channel; missing a log line
//     during a back-pressure spike is acceptable.
//   - extension snapshot sources (full-state-on-every-change, e.g. a
//     feature's db-state store): control. Fan-ins block on out so
//     back-pressure reaches the upstream Subscribe. Upstream subscribe
//     buffers coalesce stale snapshots (buffer=1, drain-then-push) so a
//     slow client gets the latest state — never gets dropped — and the
//     terminal frame always reaches the UI.
//   - history and extension operation feeds (incremental,
//     terminal-frame-matters): control. Fan-ins block on out for
//     back-pressure. Upstream uses a larger buffer because coalescing
//     would lose intermediate records / output lines; a sufficiently
//     stalled client still gets dropped to keep the daemon healthy.
type Event struct {
	Type string
	Data any
}

// handleEvents multiplexes every server-side subscription into a single SSE
// connection. Avoids the HTTP/1.1 6-per-origin connection cap that would
// otherwise saturate when each feature opens its own EventSource.
//
// Frame format per event:
//
//	event: <type>
//	data: <json>
//
// Initial frames replay the current snapshot of each source so a fresh
// subscriber catches up immediately — matching the previous per-stream
// behaviour where each EventSource's first message was the current state.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if requireMethod(w, r, http.MethodGet) {
		return
	}
	sse, err := openSSE(w)
	if err != nil {
		return
	}

	out := make(chan Event, 256)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go s.fanInStatus(ctx, out)
	go s.fanInLogs(ctx, out)
	if s.history != nil {
		ch, cancel := s.history.Subscribe()
		go forwardChan(ctx, out, "history", ch, cancel)
	}
	// Feature-registered sources (extension SSE hooks):
	// each Run takes a fresh subscription for this connection and emits
	// with the same blocking back-pressure forwardChan provides; emit
	// reports false once the connection is gone so the source stops
	// instead of draining one more value.
	for _, src := range s.extHooks.EventSources {
		name := src.Name
		go src.Run(ctx, func(data any) bool {
			select {
			case out <- Event{Type: name, Data: data}:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}
	// s.tracing is always set by NewServer (unlike history above, which
	// can fail to initialise); the store exists even when tracing is disabled.
	traceCh, traceCancel := s.tracing.Subscribe()
	go forwardChan(ctx, out, "trace", traceCh, traceCancel)

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-out:
			data, err := json.Marshal(evt.Data)
			if err != nil {
				slog.Error("event marshal", "component", "events", "type", evt.Type, "err", err)
				continue
			}
			if err := sse.SendEvent(evt.Type, data); err != nil {
				return
			}
		}
	}
}

// forwardChan reads each value off ch, wraps it as an Event with the given
// type, and forwards it onto out. Blocks on the out send so back-pressure
// reaches upstream — see the Event type comment for which sources rely on
// this vs. drop-on-full semantics.
func forwardChan[T any](ctx context.Context, out chan<- Event, evType string, ch <-chan T, cancel func()) {
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-ch:
			if !ok {
				return
			}
			select {
			case out <- Event{Type: evType, Data: v}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// fanInStatus drives the status-snapshot ticker and pushes frames onto out.
// Drops on a full out channel — the client is slow and will catch up on the
// next tick.
func (s *Server) fanInStatus(ctx context.Context, out chan<- Event) {
	push := func() {
		resp := s.buildStatusResponse()
		select {
		case out <- Event{Type: "status", Data: resp}:
		case <-ctx.Done():
		default:
		}
	}
	push()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			push()
		}
	}
}

// fanInLogs forwards service log lines onto out. Drops on a full out channel
// — log lines are observational; missing one during a back-pressure spike is
// acceptable.
func (s *Server) fanInLogs(ctx context.Context, out chan<- Event) {
	unsub := s.app.Logs.Subscribe(func(svc string, line string) {
		select {
		case out <- Event{Type: "log", Data: map[string]string{"service": svc, "line": line}}:
		case <-ctx.Done():
		default:
		}
	})
	defer unsub()
	<-ctx.Done()
}

// buildStatusResponse is the snapshot computation extracted from the old
// sendStatus helper so the events fan-in can use it without holding an
// sseWriter.
func (s *Server) buildStatusResponse() StatusResponse {
	tracked := map[string]ResourceStatus{}
	services := s.app.Orchestrator.GetAllServices()
	// One immutable snapshot for the whole assembly.
	cfg := s.holder.Load()
	for i := range services {
		svc := &services[i]
		ports := getServicePorts(cfg, svc.Name, svc.Kind)
		url := ""
		if svcCfg, ok := cfg.Services[svc.Name]; ok {
			url = svcCfg.ResolveURL()
		}
		startupTime := ""
		uptime := ""
		if !svc.StartedAt.IsZero() && !svc.HealthyAt.IsZero() {
			startupTime = formatDuration(svc.HealthyAt.Sub(svc.StartedAt))
		}
		uptimeFrom := svc.HealthyAt
		if svc.Kind == "container" && !svc.ContainerStartedAt.IsZero() {
			uptimeFrom = svc.ContainerStartedAt
		}
		if !uptimeFrom.IsZero() && svc.State == engine.StateHealthy && !svc.ExpectingContainerStart {
			uptime = formatDuration(time.Since(uptimeFrom))
		}
		lastRestart := resourceLastRestart(svc)
		sidecars := getSidecarInfos(cfg, svc.Name, svc.Kind)
		image := getContainerImage(cfg, svc.Name, svc.Kind)
		tracked[svc.Name] = ResourceStatus{
			Name:                 svc.Name,
			Kind:                 ResourceKind(svc.Kind),
			Role:                 getResourceRole(cfg, svc.Name, svc.Kind),
			State:                svc.State.String(),
			StateReason:          svc.StateReason,
			FailureKind:          string(svc.FailureKind),
			FailureEvidence:      svc.FailureEvidence,
			PortConflict:         resourcePortConflict(svc),
			LogsAvailable:        resourceLogsAvailable(s.app.Logs, svc.Name),
			RestartCount:         svc.RestartCount,
			ExternalRestartCount: svc.ExternalRestartCount,
			LastRestart:          lastRestart,
			Ports:                ports,
			URL:                  url,
			Image:                image,
			StartupTime:          startupTime,
			Uptime:               uptime,
			Sidecars:             sidecars,
		}
	}
	stale, staleReason := s.configStale()
	resp := StatusResponse{Epoch: s.epoch(), Resources: make([]ResourceStatus, 0),
		ConfigPath:  s.ConfigPath(),
		Context:     s.environmentContext(),
		ConfigStale: stale, ConfigStaleReason: staleReason}
	for name, c := range cfg.Containers {
		if ss, ok := tracked[name]; ok {
			resp.Resources = append(resp.Resources, ss)
			continue
		}
		ports := make(map[string]int, len(c.Ports))
		for label, p := range c.Ports {
			ports[label] = p.Host
		}
		sidecars := getSidecarInfos(cfg, name, "container")
		resp.Resources = append(resp.Resources, ResourceStatus{
			Name: name, Kind: ResourceKindContainer, State: engine.StateStopped.String(),
			Role: c.ResolveKind(), Ports: ports, Image: c.Image, Sidecars: sidecars,
		})
	}
	for name, svc := range cfg.Services {
		if ss, ok := tracked[name]; ok {
			resp.Resources = append(resp.Resources, ss)
			continue
		}
		ports := make(map[string]int, len(svc.Ports))
		for label, p := range svc.Ports {
			ports[label] = p.Host
		}
		resp.Resources = append(resp.Resources, ResourceStatus{
			Name: name, Kind: ResourceKindService, State: engine.StateStopped.String(),
			Role: svc.ResolveKind(), Ports: ports, URL: svc.ResolveURL(),
		})
	}
	return resp
}
