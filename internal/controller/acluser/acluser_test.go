package acluser

import (
	"testing"

	"github.com/lukasMeko/provider-valkey/apis/v1alpha1"
	vkclient "github.com/lukasMeko/provider-valkey/internal/clients"
)

func boolPtr(b bool) *bool { return &b }

func TestBuildRules(t *testing.T) {
	tests := []struct {
		name     string
		params   v1alpha1.ACLUserParameters
		password string
		want     []string
	}{
		{
			name:     "enabled with rules",
			params:   v1alpha1.ACLUserParameters{Enabled: boolPtr(true), Rules: []string{"~app:*", "+@read"}},
			password: "secret",
			want:     []string{"reset", "on", ">secret", "~app:*", "+@read"},
		},
		{
			name:     "disabled",
			params:   v1alpha1.ACLUserParameters{Enabled: boolPtr(false)},
			password: "pass",
			want:     []string{"reset", "off", ">pass"},
		},
		{
			name:     "nil enabled defaults to on",
			params:   v1alpha1.ACLUserParameters{},
			password: "pw",
			want:     []string{"reset", "on", ">pw"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRules(tt.params, tt.password)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("rule[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildRulesWithoutPassword(t *testing.T) {
	tests := []struct {
		name   string
		params v1alpha1.ACLUserParameters
		want   []string
	}{
		{
			name:   "enabled with rules",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true), Rules: []string{"~key:*", "+get"}},
			want:   []string{"resetkeys", "resetchannels", "nocommands", "on", "~key:*", "+get"},
		},
		{
			name:   "disabled no rules",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(false)},
			want:   []string{"resetkeys", "resetchannels", "nocommands", "off"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRulesWithoutPassword(tt.params)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("rule[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestContainsFlag(t *testing.T) {
	if !containsFlag([]string{"on", "allkeys"}, "on") {
		t.Error("expected true for existing flag")
	}
	if containsFlag([]string{"on", "allkeys"}, "off") {
		t.Error("expected false for missing flag")
	}
	if containsFlag(nil, "on") {
		t.Error("expected false for nil slice")
	}
}

func TestGeneratePassword(t *testing.T) {
	p1, err := generatePassword()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p1) == 0 {
		t.Fatal("password should not be empty")
	}

	// passwords should be unique
	p2, _ := generatePassword()
	if p1 == p2 {
		t.Error("consecutive passwords should differ")
	}
}

func TestClassifyRules(t *testing.T) {
	keys, cmds, channels := classifyRules([]string{
		"~app:*", "+@read", "-@dangerous", "&notifications:*", "allkeys", "allchannels", "allcommands",
	})

	wantKeys := []string{"~app:*", "~*"}
	wantCmds := []string{"+@read", "-@dangerous", "allcommands"}
	wantChannels := []string{"&notifications:*", "&*"}

	assertSlice(t, "keys", keys, wantKeys)
	assertSlice(t, "commands", cmds, wantCmds)
	assertSlice(t, "channels", channels, wantChannels)
}

func TestCommandsMatch(t *testing.T) {
	tests := []struct {
		name     string
		desired  []string
		observed string
		want     bool
	}{
		{
			name:     "exact match",
			desired:  []string{"+@all", "-@dangerous"},
			observed: "+@all -@dangerous",
			want:     true,
		},
		{
			name:     "different order matches",
			desired:  []string{"-@dangerous", "+@all"},
			observed: "+@all -@dangerous",
			want:     true,
		},
		{
			name:     "allcommands alias",
			desired:  []string{"allcommands"},
			observed: "+@all",
			want:     true,
		},
		{
			name:     "nocommands alias",
			desired:  []string{"nocommands"},
			observed: "-@all",
			want:     true,
		},
		{
			name:     "empty desired empty observed",
			desired:  nil,
			observed: "",
			want:     true,
		},
		{
			name:     "empty desired with reset state",
			desired:  nil,
			observed: "-@all",
			want:     true,
		},
		{
			name:     "mismatch",
			desired:  []string{"+@all"},
			observed: "+@read",
			want:     false,
		},
		{
			name:     "extra observed commands",
			desired:  []string{"+@read"},
			observed: "+@read +@write",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandsMatch(tt.desired, tt.observed); got != tt.want {
				t.Errorf("commandsMatch(%v, %q) = %v, want %v", tt.desired, tt.observed, got, tt.want)
			}
		})
	}
}

func TestPatternsMatch(t *testing.T) {
	tests := []struct {
		name     string
		desired  []string
		observed string
		want     bool
	}{
		{
			name:     "matching keys",
			desired:  []string{"~app:*"},
			observed: "~app:*",
			want:     true,
		},
		{
			name:     "empty desired empty observed",
			desired:  nil,
			observed: "",
			want:     true,
		},
		{
			name:     "empty desired non-empty observed detects drift",
			desired:  nil,
			observed: "~*",
			want:     false,
		},
		{
			name:     "multiple patterns",
			desired:  []string{"~app:*", "~cache:*"},
			observed: "~app:* ~cache:*",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := patternsMatch(tt.desired, tt.observed); got != tt.want {
				t.Errorf("patternsMatch(%v, %q) = %v, want %v", tt.desired, tt.observed, got, tt.want)
			}
		})
	}
}

func TestIsUpToDate(t *testing.T) {
	tests := []struct {
		name   string
		params v1alpha1.ACLUserParameters
		info   *vkclient.ACLUserInfo
		want   bool
	}{
		{
			name:   "fully matching",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true), Rules: []string{"~app:*", "+@read"}},
			info:   &vkclient.ACLUserInfo{Flags: []string{"on"}, Keys: "~app:*", Commands: "+@read", Channels: ""},
			want:   true,
		},
		{
			name:   "enabled mismatch",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true)},
			info:   &vkclient.ACLUserInfo{Flags: []string{"off"}},
			want:   false,
		},
		{
			name:   "key drift detected",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true)},
			info:   &vkclient.ACLUserInfo{Flags: []string{"on"}, Keys: "~*"},
			want:   false,
		},
		{
			name:   "command drift detected",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true)},
			info:   &vkclient.ACLUserInfo{Flags: []string{"on"}, Commands: "+@all"},
			want:   false,
		},
		{
			name:   "empty rules with clean state",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true)},
			info:   &vkclient.ACLUserInfo{Flags: []string{"on"}, Keys: "", Commands: "-@all", Channels: ""},
			want:   true,
		},
		{
			name:   "empty rules with empty commands",
			params: v1alpha1.ACLUserParameters{Enabled: boolPtr(true)},
			info:   &vkclient.ACLUserInfo{Flags: []string{"on"}, Keys: "", Commands: "", Channels: ""},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpToDate(tt.params, tt.info); got != tt.want {
				t.Errorf("isUpToDate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func assertSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", name, i, got[i], want[i])
		}
	}
}
