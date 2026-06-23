package contextpackets

import "testing"

func TestStatusFromCoverage(t *testing.T) {
	cases := []struct {
		name      string
		entries   []ContextCoverageEntry
		truncated bool
		want      string
	}{
		{
			name: "created",
			entries: []ContextCoverageEntry{
				{Status: CoverageStatusCovered, Required: true},
			},
			want: ContextPacketStatusCreated,
		},
		{
			name: "required blocked",
			entries: []ContextCoverageEntry{
				{Status: CoverageStatusBlocked, Required: true},
			},
			want: ContextPacketStatusBlocked,
		},
		{
			name: "required missing",
			entries: []ContextCoverageEntry{
				{Status: CoverageStatusMissing, Required: true},
			},
			want: ContextPacketStatusBlocked,
		},
		{
			name: "optional partial",
			entries: []ContextCoverageEntry{
				{Status: CoverageStatusPartial},
			},
			want: ContextPacketStatusPartial,
		},
		{
			name: "truncated",
			entries: []ContextCoverageEntry{
				{Status: CoverageStatusCovered, Required: true},
			},
			truncated: true,
			want:      ContextPacketStatusPartial,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusFromCoverage(tc.entries, tc.truncated); got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}
