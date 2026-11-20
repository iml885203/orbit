package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type LifecycleOperation struct {
	Warnings   []string   `json:"warnings,omitempty"`
	Cleanup    string     `json:"cleanup,omitempty"`
	ID         string     `json:"id"`
	TargetMode string     `json:"target_mode,omitempty"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Phase      string     `json:"phase"`
	Epoch      int64      `json:"epoch"`
	Generation uint64     `json:"generation"`
	ConfigPath string     `json:"config_path"`
	Instance   string     `json:"instance,omitempty"`
	Resources  []string   `json:"resources"`
	AcceptedAt time.Time  `json:"accepted_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Code       string     `json:"code,omitempty"`
}

func (c *Client) Operation(id string) (*LifecycleOperation, error) {
	response, err := c.get("http://orbit/api/operations/" + url.PathEscape(id))
	if err != nil {
		return nil, fmt.Errorf("lifecycle operation request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, readError(response)
	}
	var operation LifecycleOperation
	if err := json.NewDecoder(response.Body).Decode(&operation); err != nil {
		return nil, fmt.Errorf("decode lifecycle operation: %w", err)
	}
	return &operation, nil
}

const ExpectedEpochHeader = "X-Orbit-Expected-Epoch"
const ExpectedGenerationHeader = "X-Orbit-Expected-Generation"

func (c *Client) WithEnvironment(epoch int64, generation uint64) *Client {
	clone := *c
	clone.expectedEpoch = epoch
	clone.expectedGeneration = &generation
	return &clone
}
