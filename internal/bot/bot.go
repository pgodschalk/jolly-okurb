package bot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"jolly-okurb/internal/config"
)

const (
	SkullEmoji       = "💀"
	HistoricalCutoff = "2025-01-01T00:00:00Z"
)

type Bot struct {
	config    *config.Config
	channelID string
	ready     bool
	mu        sync.RWMutex
	cancel    context.CancelFunc
}

func New(cfg *config.Config) *Bot {
	return &Bot{config: cfg}
}

// Initialize resolves the channel ID before the bot starts processing events.
func (b *Bot) Initialize(s Session) error {
	channels, err := s.GuildChannels(b.config.GuildID)
	if err != nil {
		return fmt.Errorf("failed to fetch guild channels: %w", err)
	}

	channelID := FindChannelByName(channels, b.config.ChannelName)
	if channelID == "" {
		return fmt.Errorf("channel '%s' not found in guild", b.config.ChannelName)
	}

	b.mu.Lock()
	b.channelID = channelID
	b.ready = true
	b.mu.Unlock()

	slog.Info("monitoring channel", "channel", b.config.ChannelName, "id", b.channelID)
	return nil
}

func (b *Bot) OnReady(s *discordgo.Session, event *discordgo.Ready) {
	slog.Info("logged in", "username", event.User.Username, "discriminator", event.User.Discriminator)

	if err := b.Initialize(s); err != nil {
		slog.Error("initialization failed", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
	go b.ProcessHistoricalMessages(ctx, s)
}

func (b *Bot) Shutdown() {
	b.mu.RLock()
	cancel := b.cancel
	b.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Bot) OnReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if !b.ShouldProcessReaction(r) {
		return
	}

	slog.Debug("detected skull reaction from target user", "message_id", r.MessageID)
	b.ReplaceReaction(s, r.MessageID)
}

func (b *Bot) ShouldProcessReaction(r *discordgo.MessageReactionAdd) bool {
	b.mu.RLock()
	ready := b.ready
	channelID := b.channelID
	b.mu.RUnlock()

	if !ready {
		return false
	}
	if r.ChannelID != channelID {
		return false
	}
	if r.UserID != b.config.TargetUserID {
		return false
	}
	if r.Emoji.Name != SkullEmoji {
		return false
	}
	return true
}

func (b *Bot) ProcessHistoricalMessages(ctx context.Context, s Session) {
	cutoff, err := time.Parse(time.RFC3339, HistoricalCutoff)
	if err != nil {
		slog.Error("invalid historical cutoff date", "error", err)
		return
	}
	slog.Info("processing historical messages", "cutoff", cutoff.Format("2006-01-02"))

	var beforeID string
	processed := 0
	replaced := 0

	for {
		select {
		case <-ctx.Done():
			slog.Info("historical processing cancelled", "processed", processed, "replaced", replaced)
			return
		default:
		}

		messages, err := s.ChannelMessages(b.channelID, 100, beforeID, "", "")
		if err != nil {
			slog.Error("failed to fetch messages", "error", err)
			break
		}

		if len(messages) == 0 {
			break
		}

		for _, msg := range messages {
			if msg.Timestamp.Before(cutoff) {
				slog.Info("reached messages before cutoff", "processed", processed, "replaced", replaced)
				return
			}

			count := b.ProcessMessageReactions(s, msg)
			replaced += count
			processed++
		}

		beforeID = messages[len(messages)-1].ID

		// Log progress periodically
		if processed%500 == 0 {
			slog.Info("historical processing progress", "processed", processed, "replaced", replaced)
		}

		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("historical processing complete", "processed", processed, "replaced", replaced)
}

func (b *Bot) ProcessMessageReactions(s Session, msg *discordgo.Message) int {
	replaced := 0

	for _, reaction := range msg.Reactions {
		if reaction.Emoji.Name != SkullEmoji {
			continue
		}

		if b.hasTargetUserReaction(s, msg.ID) {
			if b.ReplaceReaction(s, msg.ID) {
				replaced++
			}
		}
	}

	return replaced
}

// hasTargetUserReaction paginates through all reactions to find the target user.
func (b *Bot) hasTargetUserReaction(s Session, messageID string) bool {
	var afterID string

	for {
		users, err := s.MessageReactions(b.channelID, messageID, SkullEmoji, 100, "", afterID)
		if err != nil {
			slog.Error("failed to fetch reactions", "message_id", messageID, "error", err)
			return false
		}

		if len(users) == 0 {
			return false
		}

		if HasUser(users, b.config.TargetUserID) {
			return true
		}

		// No more pages if we got fewer than requested
		if len(users) < 100 {
			return false
		}

		afterID = users[len(users)-1].ID
	}
}

func (b *Bot) ReplaceReaction(s Session, messageID string) bool {
	err := s.MessageReactionRemove(b.channelID, messageID, SkullEmoji, b.config.TargetUserID)
	if err != nil {
		slog.Error("failed to remove skull reaction", "message_id", messageID, "error", err)
		return false
	}

	err = s.MessageReactionAdd(b.channelID, messageID, b.config.JollySkullID)
	if err != nil {
		slog.Error("failed to add jollyskull reaction", "message_id", messageID, "error", err)
		return false
	}

	slog.Debug("replaced skull with jollyskull", "message_id", messageID)
	return true
}

func FindChannelByName(channels []*discordgo.Channel, name string) string {
	for _, ch := range channels {
		if ch.Name == name && ch.Type == discordgo.ChannelTypeGuildText {
			return ch.ID
		}
	}
	return ""
}

func HasUser(users []*discordgo.User, userID string) bool {
	for _, user := range users {
		if user.ID == userID {
			return true
		}
	}
	return false
}
