package tunnel

import (
	"reflect"
	"testing"
)

func TestBuildTunnelListJSONDataCarriesClaimsInEnvelopeShape(t *testing.T) {
	state := TunnelListResponse{Claims: []LocalClaimView{{
		Paths: []string{"/callbacks/a"}, LocalPort: 8080, Status: "connected", Owner: "logan", StartedAt: "t0",
	}}}
	data := buildTunnelListJSONData(state)
	if data.Operation != "tunnel_list" || len(data.Claims) != 1 {
		t.Fatalf("data = %+v", data)
	}
	claim := data.Claims[0]
	if claim.Target != "localhost:8080" || !claim.Mine || claim.Status != "connected" {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestBuildGlobalClaimsJSONDataMarksOwnership(t *testing.T) {
	data := buildGlobalClaimsJSONData([]GlobalClaimView{
		{PathPrefix: "/callbacks/x", Owner: "logan", Mine: true},
		{PathPrefix: "/callbacks/y", Owner: "sam", Mine: false},
	})
	if len(data.Claims) != 2 || !data.Claims[0].Mine || data.Claims[1].Mine {
		t.Fatalf("claims = %+v", data.Claims)
	}
	if !reflect.DeepEqual(data.Claims[1].Paths, []string{"/callbacks/y"}) {
		t.Fatalf("paths = %+v", data.Claims[1].Paths)
	}
}
