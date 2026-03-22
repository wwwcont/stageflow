package app

import "testing"

func TestExecutorHostPolicies(t *testing.T) {
	tests := []struct {
		name              string
		allowedHosts      []string
		wantLoopbackHosts bool
		wantIPHosts       bool
	}{
		{name: "localhost enables loopback", allowedHosts: []string{"example.internal", "localhost"}, wantLoopbackHosts: true},
		{name: "loopback ip enables both flags", allowedHosts: []string{"127.0.0.1"}, wantLoopbackHosts: true, wantIPHosts: true},
		{name: "regular host keeps strict defaults", allowedHosts: []string{"example.internal"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLoopbackHosts, gotIPHosts := executorHostPolicies(tt.allowedHosts)
			if gotLoopbackHosts != tt.wantLoopbackHosts || gotIPHosts != tt.wantIPHosts {
				t.Fatalf("executorHostPolicies(%#v) = (%t, %t), want (%t, %t)", tt.allowedHosts, gotLoopbackHosts, gotIPHosts, tt.wantLoopbackHosts, tt.wantIPHosts)
			}
		})
	}
}
