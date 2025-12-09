package config

import (
	"fmt"
	"os"
)

type Config struct {
	Token        string // Discord bot token
	GuildID      string // Server ID to operate in
	ChannelName  string // Channel name to monitor
	TargetUserID string // User ID whose reactions to replace
	JollySkullID string // Custom emoji ID for jollyskull
}

func Load() (*Config, error) {
	cfg := &Config{
		Token:        os.Getenv("DISCORD_TOKEN"),
		GuildID:      os.Getenv("DISCORD_GUILD_ID"),
		ChannelName:  os.Getenv("DISCORD_CHANNEL_NAME"),
		TargetUserID: os.Getenv("DISCORD_TARGET_USER_ID"),
		JollySkullID: os.Getenv("DISCORD_JOLLYSKULL_ID"),
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
	if cfg.TargetUserID == "" {
		return nil, fmt.Errorf("DISCORD_TARGET_USER_ID is required")
	}
	if cfg.JollySkullID == "" {
		return nil, fmt.Errorf("DISCORD_JOLLYSKULL_ID is required")
	}

	return cfg, nil
}
