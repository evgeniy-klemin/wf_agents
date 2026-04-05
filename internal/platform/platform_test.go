package platform

import "testing"

func TestGitRemoteToWebURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "SSH with .git suffix",
			input: "git@gitlab.diftech.org:evgeniy.klemin/wf_agents.git",
			want:  "https://gitlab.diftech.org/evgeniy.klemin/wf_agents",
		},
		{
			name:  "HTTPS with .git suffix",
			input: "https://gitlab.diftech.org/evgeniy.klemin/wf_agents.git",
			want:  "https://gitlab.diftech.org/evgeniy.klemin/wf_agents",
		},
		{
			name:  "HTTPS without .git suffix",
			input: "https://gitlab.diftech.org/evgeniy.klemin/wf_agents",
			want:  "https://gitlab.diftech.org/evgeniy.klemin/wf_agents",
		},
		{
			name:  "GitHub SSH URL",
			input: "git@github.com:octocat/Hello-World.git",
			want:  "https://github.com/octocat/Hello-World",
		},
		{
			name:  "GitHub HTTPS URL",
			input: "https://github.com/octocat/Hello-World.git",
			want:  "https://github.com/octocat/Hello-World",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "malformed SSH (no colon)",
			input: "git@github.com/no-colon",
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GitRemoteToWebURL(tc.input)
			if got != tc.want {
				t.Errorf("GitRemoteToWebURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestProjectNameFromURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "SSH with .git suffix",
			input: "git@gitlab.diftech.org:evgeniy.klemin/wf_agents.git",
			want:  "wf_agents",
		},
		{
			name:  "HTTPS with .git suffix",
			input: "https://gitlab.diftech.org/evgeniy.klemin/wf_agents.git",
			want:  "wf_agents",
		},
		{
			name:  "HTTPS without .git suffix",
			input: "https://github.com/octocat/Hello-World",
			want:  "Hello-World",
		},
		{
			name:  "GitHub SSH URL",
			input: "git@github.com:octocat/Hello-World.git",
			want:  "Hello-World",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no slash or colon",
			input: "myrepo.git",
			want:  "myrepo",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ProjectNameFromURL(tc.input)
			if got != tc.want {
				t.Errorf("ProjectNameFromURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
