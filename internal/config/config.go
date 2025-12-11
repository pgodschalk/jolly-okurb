package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Token         string   // Discord bot token
	GuildID       string   // Server ID to operate in
	ChannelName   string   // Channel name to monitor
	TargetUserIDs []string // User IDs whose reactions to replace
	JollySkullID  string   // Custom emoji ID for jollyskull
}

func Load() (*Config, error) {
	cfg := &Config{
		Token:        os.Getenv("DISCORD_TOKEN"),
		GuildID:      os.Getenv("DISCORD_GUILD_ID"),
		ChannelName:  os.Getenv("DISCORD_CHANNEL_NAME"),
		JollySkullID: os.Getenv("DISCORD_JOLLYSKULL_ID"),
	}

	// Parse comma-separated user IDs
	targetUserIDs := os.Getenv("DISCORD_TARGET_USER_IDS")
	if targetUserIDs == "" {
		// Fall back to singular for backwards compatibility
		targetUserIDs = os.Getenv("DISCORD_TARGET_USER_ID")
	}
	if targetUserIDs != "" {
		for _, id := range strings.Split(targetUserIDs, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				cfg.TargetUserIDs = append(cfg.TargetUserIDs, id)
			}
		}
	}

	if cfg.Token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is required")
	}
	if cfg.GuildID == "" {
		return nil, fmt.Errorf("DISCORD_GUILD_ID is required")
	}
	if cfg.ChannelName == "" {
		cfg.ChannelName = "jollyposting"
	}
	if len(cfg.TargetUserIDs) == 0 {
		return nil, fmt.Errorf("DISCORD_TARGET_USER_IDS is required")
	}
	if cfg.JollySkullID == "" {
		return nil, fmt.Errorf("DISCORD_JOLLYSKULL_ID is required")
	}

	return cfg, nil
}
