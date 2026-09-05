package runtime

import "testing"

func TestResolvePrincipalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pusherName string
		explicit   string
		want       string
	}{
		{
			name:       "uses explicit principal",
			pusherName: "docker-main",
			explicit:   "custom-principal",
			want:       "custom-principal",
		},
		{
			name:       "derives principal from pusher name",
			pusherName: "docker-main",
			want:       "guardian-pusher-docker-main",
		},
		{
			name: "falls back when pusher name empty",
			want: "guardian-pusher",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolvePrincipalID(tc.pusherName, tc.explicit); got != tc.want {
				t.Fatalf("ResolvePrincipalID(%q, %q) = %q, want %q", tc.pusherName, tc.explicit, got, tc.want)
			}
		})
	}
}
