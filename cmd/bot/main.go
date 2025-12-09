package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"

	"jolly-okurb/internal/bot"
	"jolly-okurb/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	dg, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		slog.Error("failed to create Discord session", "error", err)
		os.Exit(1)
	}

	// Enable automatic rate limit handling
	dg.ShouldRetryOnRateLimit = true
	dg.MaxRestRetries = 3

	b := bot.New(cfg)

	dg.AddHandler(b.OnReady)
	dg.AddHandler(b.OnReactionAdd)

	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentGuildMembers

	if err := dg.Open(); err != nil {
		slog.Error("failed to open connection", "error", err)
		os.Exit(1)
	}
	defer dg.Close()

	slog.Info("bot is running")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	slog.Info("shutting down")
	b.Shutdown()
}
