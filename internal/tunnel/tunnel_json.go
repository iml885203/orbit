package tunnel

import "fmt"

// tunnel list/release speak the orbit.cli.v1 envelope under the global
// --json; the upstream tunlease event shape stays available behind
// -o json for callers of the embedded client. claim keeps NDJSON in both
// modes — it is a stream, not a request-response.

type tunnelClaimJSON struct {
	Paths     []string `json:"paths"`
	Owner     string   `json:"owner,omitempty"`
	StartedAt string   `json:"started_at,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Target    string   `json:"target,omitempty"`
	LocalPort int      `json:"local_port,omitempty"`
	Mine      bool     `json:"mine"`
	Status    string   `json:"status,omitempty"`
}

type tunnelListJSONData struct {
	Operation string            `json:"operation"`
	Claims    []tunnelClaimJSON `json:"claims"`
}

func buildTunnelListJSONData(state TunnelListResponse) tunnelListJSONData {
	claims := make([]tunnelClaimJSON, 0, len(state.Claims))
	for _, claim := range state.Claims {
		claims = append(claims, tunnelClaimJSON{
			Paths:     claim.Paths,
			Owner:     claim.Owner,
			StartedAt: claim.StartedAt,
			ExpiresAt: claim.ExpiresAt,
			Target:    fmt.Sprintf("localhost:%d", claim.LocalPort),
			LocalPort: claim.LocalPort,
			Mine:      true,
			Status:    claim.Status,
		})
	}
	return tunnelListJSONData{Operation: "tunnel_list", Claims: claims}
}

func buildGlobalClaimsJSONData(claims []GlobalClaimView) tunnelListJSONData {
	items := make([]tunnelClaimJSON, 0, len(claims))
	for _, claim := range claims {
		items = append(items, tunnelClaimJSON{
			Paths:     []string{claim.PathPrefix},
			Owner:     claim.Owner,
			StartedAt: claim.StartedAt,
			ExpiresAt: claim.ExpiresAt,
			Mine:      claim.Mine,
			Status:    "connected",
		})
	}
	return tunnelListJSONData{Operation: "tunnel_list", Claims: items}
}

type tunnelReleaseJSONData struct {
	Operation     string   `json:"operation"`
	Released      int      `json:"released"`
	ReleasedPaths []string `json:"released_paths,omitempty"`
	LocalPort     int      `json:"local_port,omitempty"`
	Gateway       string   `json:"gateway,omitempty"`
}
