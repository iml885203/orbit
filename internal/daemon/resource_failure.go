package daemon

func resourceFailureSummary(status ResourceStatus) string {
	if status.FailureEvidence == "" || status.FailureEvidence == status.StateReason {
		return status.StateReason
	}
	if status.StateReason == "" {
		return status.FailureEvidence
	}
	return status.StateReason + " — " + status.FailureEvidence
}
