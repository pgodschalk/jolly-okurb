package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

	slog.Debug("detected skull reaction from target user", "message_id", r.MessageID, "user_id", r.UserID)
	b.ReplaceReaction(s, r.MessageID, r.UserID)
}

func (b *Bot) OnMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !b.ShouldDeleteMessage(m) {
		return
	}

	slog.Debug("detected skull-only message from target user", "message_id", m.ID)
	if err := s.ChannelMessageDelete(m.ChannelID, m.ID); err != nil {
		slog.Error("failed to delete message", "message_id", m.ID, "error", err)
		return
	}
	slog.Info("deleted skull-only message", "message_id", m.ID)
}

func (b *Bot) ShouldDeleteMessage(m *discordgo.MessageCreate) bool {
	b.mu.RLock()
	ready := b.ready
	channelID := b.channelID
	b.mu.RUnlock()

	if !ready {
		return false
	}
	if m.ChannelID != channelID {
		return false
	}
	if m.Author == nil || !b.IsTargetUser(m.Author.ID) {
		return false
	}
	// Check if message contains only skull emojis (with optional whitespace)
	content := strings.ReplaceAll(m.Content, " ", "")
	if content == "" {
		return false
	}
	content = strings.ReplaceAll(content, SkullEmoji, "")
	return content == ""
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
	if !b.IsTargetUser(r.UserID) {
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

		targetUsers := b.findTargetUsersWithReaction(s, msg.ID)
		for _, userID := range targetUsers {
			if b.ReplaceReaction(s, msg.ID, userID) {
				replaced++
			}
		}
	}

	return replaced
}

// findTargetUsersWithReaction paginates through all reactions to find target users.
// Returns the list of target user IDs that have reacted with the skull emoji.
func (b *Bot) findTargetUsersWithReaction(s Session, messageID string) []string {
	var afterID string
	var found []string

	for {
		users, err := s.MessageReactions(b.channelID, messageID, SkullEmoji, 100, "", afterID)
		if err != nil {
			slog.Error("failed to fetch reactions", "message_id", messageID, "error", err)
			return found
		}

		if len(users) == 0 {
			return found
		}

		for _, user := range users {
			if b.IsTargetUser(user.ID) {
				found = append(found, user.ID)
			}
		}

		// No more pages if we got fewer than requested
		if len(users) < 100 {
			return found
		}

		afterID = users[len(users)-1].ID
	}
}

func (b *Bot) ReplaceReaction(s Session, messageID, userID string) bool {
	err := s.MessageReactionRemove(b.channelID, messageID, SkullEmoji, userID)
	if err != nil {
		slog.Error("failed to remove skull reaction", "message_id", messageID, "user_id", userID, "error", err)
		return false
	}

	err = s.MessageReactionAdd(b.channelID, messageID, b.config.JollySkullID)
	if err != nil {
		slog.Error("failed to add jollyskull reaction", "message_id", messageID, "error", err)
		return false
	}

	slog.Debug("replaced skull with jollyskull", "message_id", messageID, "user_id", userID)
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

// IsTargetUser checks if the given user ID is in the target user list.
func (b *Bot) IsTargetUser(userID string) bool {
	for _, id := range b.config.TargetUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}
