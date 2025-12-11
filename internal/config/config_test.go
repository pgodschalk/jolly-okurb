package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantErr     bool
		errContains string
		validate    func(*testing.T, *Config)
	}{
		{
			name: "valid config with all fields",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_CHANNEL_NAME":    "test-channel",
				"DISCORD_TARGET_USER_IDS": "user-456",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Token != "test-token" {
					t.Errorf("Token = %q, want %q", cfg.Token, "test-token")
				}
				if cfg.GuildID != "guild-123" {
					t.Errorf("GuildID = %q, want %q", cfg.GuildID, "guild-123")
				}
				if cfg.ChannelName != "test-channel" {
					t.Errorf("ChannelName = %q, want %q", cfg.ChannelName, "test-channel")
				}
				expected := []string{"user-456"}
				if !reflect.DeepEqual(cfg.TargetUserIDs, expected) {
					t.Errorf("TargetUserIDs = %v, want %v", cfg.TargetUserIDs, expected)
				}
				if cfg.JollySkullID != "jollyskull:789" {
					t.Errorf("JollySkullID = %q, want %q", cfg.JollySkullID, "jollyskull:789")
				}
			},
		},
		{
			name: "multiple target user IDs",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_IDS": "user-1,user-2,user-3",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"user-1", "user-2", "user-3"}
				if !reflect.DeepEqual(cfg.TargetUserIDs, expected) {
					t.Errorf("TargetUserIDs = %v, want %v", cfg.TargetUserIDs, expected)
				}
				// Verify set is populated for O(1) lookup
				for _, id := range expected {
					if _, ok := cfg.TargetUserIDSet[id]; !ok {
						t.Errorf("TargetUserIDSet missing %q", id)
					}
				}
				if len(cfg.TargetUserIDSet) != len(expected) {
					t.Errorf("TargetUserIDSet has %d entries, want %d", len(cfg.TargetUserIDSet), len(expected))
				}
			},
		},
		{
			name: "target user IDs with whitespace",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_IDS": " user-1 , user-2 , user-3 ",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"user-1", "user-2", "user-3"}
				if !reflect.DeepEqual(cfg.TargetUserIDs, expected) {
					t.Errorf("TargetUserIDs = %v, want %v", cfg.TargetUserIDs, expected)
				}
			},
		},
		{
			name: "backwards compatible with singular env var",
			envVars: map[string]string{
				"DISCORD_TOKEN":          "test-token",
				"DISCORD_GUILD_ID":       "guild-123",
				"DISCORD_TARGET_USER_ID": "user-456",
				"DISCORD_JOLLYSKULL_ID":  "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"user-456"}
				if !reflect.DeepEqual(cfg.TargetUserIDs, expected) {
					t.Errorf("TargetUserIDs = %v, want %v", cfg.TargetUserIDs, expected)
				}
			},
		},
		{
			name: "plural takes precedence over singular",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_ID":  "old-user",
				"DISCORD_TARGET_USER_IDS": "new-user-1,new-user-2",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				expected := []string{"new-user-1", "new-user-2"}
				if !reflect.DeepEqual(cfg.TargetUserIDs, expected) {
					t.Errorf("TargetUserIDs = %v, want %v", cfg.TargetUserIDs, expected)
				}
			},
		},
		{
			name: "default channel name",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_IDS": "user-456",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.ChannelName != "jollyposting" {
					t.Errorf("ChannelName = %q, want default %q", cfg.ChannelName, "jollyposting")
				}
			},
		},
		{
			name: "missing token",
			envVars: map[string]string{
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_IDS": "user-456",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr:     true,
			errContains: "DISCORD_TOKEN",
		},
		{
			name: "missing guild ID",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_TARGET_USER_IDS": "user-456",
				"DISCORD_JOLLYSKULL_ID":   "jollyskull:789",
			},
			wantErr:     true,
			errContains: "DISCORD_GUILD_ID",
		},
		{
			name: "missing target user IDs",
			envVars: map[string]string{
				"DISCORD_TOKEN":         "test-token",
				"DISCORD_GUILD_ID":      "guild-123",
				"DISCORD_JOLLYSKULL_ID": "jollyskull:789",
			},
			wantErr:     true,
			errContains: "DISCORD_TARGET_USER_IDS",
		},
		{
			name: "missing jollyskull ID",
			envVars: map[string]string{
				"DISCORD_TOKEN":           "test-token",
				"DISCORD_GUILD_ID":        "guild-123",
				"DISCORD_TARGET_USER_IDS": "user-456",
			},
			wantErr:     true,
			errContains: "DISCORD_JOLLYSKULL_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			clearEnvVars()

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer clearEnvVars()

			cfg, err := Load()

			if tt.wantErr {
				if err == nil {
					t.Fatal("Load() expected error, got nil")
				}
				if tt.errContains != "" {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func clearEnvVars() {
	os.Unsetenv("DISCORD_TOKEN")
	os.Unsetenv("DISCORD_GUILD_ID")
	os.Unsetenv("DISCORD_CHANNEL_NAME")
	os.Unsetenv("DISCORD_TARGET_USER_ID")
	os.Unsetenv("DISCORD_TARGET_USER_IDS")
	os.Unsetenv("DISCORD_JOLLYSKULL_ID")
}
