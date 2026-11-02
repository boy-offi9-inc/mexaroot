package wire

import "testing"

func TestRequiredCapability(t *testing.T) {
	cases := []struct {
		msgType string
		want    string
		known   bool
	}{
		{TypeHello, "", true},
		{TypeRuntimeList, "", true},
		{TypeExecRun, CapabilityExec, true},
		{TypeSessionStart, CapabilitySession, true},
		{TypeSessionInput, CapabilitySession, true},
		{TypeSessionResize, CapabilitySession, true},
		{TypeSessionClose, CapabilitySession, true},
		{TypeFilePush, CapabilityFile, true},
		{TypeFilePull, CapabilityFile, true},
		{TypePing, CapabilityKeepalive, true},
		{"unknown.type", "", false},
	}
	for _, tc := range cases {
		got, known := RequiredCapability(tc.msgType)
		if got != tc.want || known != tc.known {
			t.Errorf("RequiredCapability(%q) = %q, %v; want %q, %v", tc.msgType, got, known, tc.want, tc.known)
		}
	}
}
