package wire

import "testing"

func TestNegotiate(t *testing.T) {
	cases := []struct {
		name      string
		client    []int
		supported []int
		want      int
		ok        bool
	}{
		{"highest shared", []int{1, 2, 3}, []int{1, 2}, 2, true},
		{"single shared", []int{1}, []int{1}, 1, true},
		{"none shared", []int{3}, []int{1, 2}, 0, false},
		{"empty client", nil, []int{1}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Negotiate(tc.client, tc.supported)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("Negotiate(%v, %v) = %d, %v; want %d, %v", tc.client, tc.supported, got, ok, tc.want, tc.ok)
			}
		})
	}
}
