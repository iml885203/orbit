package app

import (
	"strings"

	"github.com/iml885203/orbit/daemon"
)

// The failure-evidence surface of a JSON up: everything a CI caller or
// agent needs to explain a start that did not reach healthy, without a
// follow-up orbit logs call.

type upFailureJSONData struct {
	Operation          string             `json:"operation"`
	RequestedResources []string           `json:"requested_resources"`
	FailedResources    []upFailedResource `json:"failed_resources"`
}

type upFailedResource struct {
	Name        string   `json:"name"`
	State       string   `json:"state"`
	StateReason string   `json:"state_reason,omitempty"`
	LogTail     []string `json:"log_tail,omitempty"`
}

// buildUpFailureJSONData packs the evidence for every watched resource that
// did not reach healthy — state, reason, and a bounded log tail — so a CI
// caller or agent does not need a second `orbit logs` call to explain the
// failure.
func buildUpFailureJSONData(names []string, status *daemon.StatusResponse, logTail func(string) []string) upFailureJSONData {
	data := upFailureJSONData{Operation: "up", RequestedResources: names}
	if status == nil {
		return data
	}
	watch := watchSet(names)
	for i := range status.Resources {
		svc := &status.Resources[i]
		if len(names) > 0 && !watch[svc.Name] {
			continue
		}
		if svc.State == "healthy" {
			continue
		}
		reason := svc.StateReason
		if reason == "" && svc.HealthProgress != nil {
			reason = svc.HealthProgress.LastErr
		}
		// A pending resource has no reason of its own — it never started. Left
		// empty it is indistinguishable from one that started and failed
		// silently, so name what it is waiting on.
		if reason == "" && svc.State == "pending" && len(svc.PendingDependencies) > 0 {
			reason = "waiting for " + strings.Join(svc.PendingDependencies, ", ")
		}
		data.FailedResources = append(data.FailedResources, upFailedResource{
			Name:        svc.Name,
			State:       svc.State,
			StateReason: reason,
			LogTail:     logTail(svc.Name),
		})
	}
	return data
}

func recentLogTail(client *daemon.Client, name string) []string {
	return recentLogTailWithProgress(nil, client, name)
}

func recentLogTailWithProgress(progress *lifecycleProgress, client *daemon.Client, name string) []string {
	const maxTailLines = 20
	if client == nil {
		return nil
	}
	progress.Phase(phaseCollectingFailureEvidence)
	response, err := client.Logs(name, maxTailLines)
	if err != nil || response == nil {
		return nil
	}
	return response.Lines
}

func recentLogEvidence(client *daemon.Client, name string) string {
	return recentLogEvidenceWithProgress(nil, client, name)
}

func recentLogEvidenceWithProgress(progress *lifecycleProgress, client *daemon.Client, name string) string {
	if client == nil {
		return ""
	}
	progress.Phase(phaseCollectingFailureEvidence)
	response, err := client.Logs(name, 20)
	if err != nil || response == nil {
		return ""
	}
	return lastServiceLogLine(response.Lines)
}

func lastServiceLogLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "[orbit]") {
			continue
		}
		return truncateEvidence(line)
	}
	return ""
}

func truncateEvidence(line string) string {
	const maxEvidenceLength = 240
	if len(line) <= maxEvidenceLength {
		return line
	}
	return line[:maxEvidenceLength-3] + "..."
}
