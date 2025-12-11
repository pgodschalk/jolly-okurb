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

	slog.Debug("detected skull reaction from target user", "message_id", r.MessageID, "user_id", r.UserID, "emoji", r.Emoji.Name)
	b.ReplaceReaction(s, r.MessageID, r.UserID, &r.Emoji)
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
	return b.IsSkullOnlyMessage(m.Content)
}

// IsSkullOnlyMessage checks if a message contains only skull-related emojis and whitespace.
func (b *Bot) IsSkullOnlyMessage(content string) bool {
	// Remove whitespace
	content = strings.ReplaceAll(content, " ", "")
	content = strings.ReplaceAll(content, "\n", "")
	content = strings.ReplaceAll(content, "\t", "")
	if content == "" {
		return false
	}

	// Remove Unicode skull emojis
	content = strings.ReplaceAll(content, "💀", "")
	content = strings.ReplaceAll(content, "☠️", "")
	content = strings.ReplaceAll(content, "☠", "")

	// Filter out skull custom emojis, keep everything else
	remaining := filterCustomEmojis(content, isSkullCustomEmoji)

	return remaining == ""
}

// filterCustomEmojis processes custom Discord emojis in content.
// It removes emojis where shouldRemove returns true and keeps the rest.
func filterCustomEmojis(content string, shouldRemove func(emojiTag string) bool) string {
	var result strings.Builder
	for len(content) > 0 {
		start := strings.Index(content, "<")
		if start == -1 {
			result.WriteString(content)
			break
		}

		// Keep content before the emoji tag
		result.WriteString(content[:start])
		content = content[start:]

		end := strings.Index(content, ">")
		if end == -1 {
			// Malformed tag, keep remaining content
			result.WriteString(content)
			break
		}

		emojiTag := content[:end+1]
		content = content[end+1:]

		if !shouldRemove(emojiTag) {
			result.WriteString(emojiTag)
		}
	}
	return result.String()
}

// isSkullCustomEmoji checks if a Discord custom emoji tag contains "skull" (but not "jollyskull").
// Expects format: <:name:id> or <a:name:id> for animated emojis.
func isSkullCustomEmoji(emojiTag string) bool {
	parts := strings.Split(emojiTag, ":")
	if len(parts) < 2 {
		return false
	}
	name := strings.ToLower(parts[1])
	return strings.Contains(name, "skull") && !strings.Contains(name, "jollyskull")
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
	if !b.IsSkullEmoji(&r.Emoji) {
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
		if !b.IsSkullEmoji(reaction.Emoji) {
			continue
		}

		targetUsers := b.findTargetUsersWithReaction(s, msg.ID, reaction.Emoji)
		for _, userID := range targetUsers {
			if b.ReplaceReaction(s, msg.ID, userID, reaction.Emoji) {
				replaced++
			}
		}
	}

	return replaced
}

// findTargetUsersWithReaction paginates through all reactions to find target users.
// Returns the list of target user IDs that have reacted with the given emoji.
func (b *Bot) findTargetUsersWithReaction(s Session, messageID string, emoji *discordgo.Emoji) []string {
	var afterID string
	var found []string
	emojiStr := GetEmojiAPIString(emoji)

	for {
		users, err := s.MessageReactions(b.channelID, messageID, emojiStr, 100, "", afterID)
		if err != nil {
			slog.Error("failed to fetch reactions", "message_id", messageID, "emoji", emojiStr, "error", err)
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

func (b *Bot) ReplaceReaction(s Session, messageID, userID string, emoji *discordgo.Emoji) bool {
	emojiStr := GetEmojiAPIString(emoji)
	err := s.MessageReactionRemove(b.channelID, messageID, emojiStr, userID)
	if err != nil {
		slog.Error("failed to remove skull reaction", "message_id", messageID, "user_id", userID, "emoji", emojiStr, "error", err)
		return false
	}

	err = s.MessageReactionAdd(b.channelID, messageID, b.config.JollySkullID)
	if err != nil {
		slog.Error("failed to add jollyskull reaction", "message_id", messageID, "error", err)
		return false
	}

	slog.Debug("replaced skull with jollyskull", "message_id", messageID, "user_id", userID, "emoji", emojiStr)
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

// IsTargetUser checks if the given user ID is in the target user set (O(1) lookup).
func (b *Bot) IsTargetUser(userID string) bool {
	_, ok := b.config.TargetUserIDSet[userID]
	return ok
}

// IsSkullEmoji checks if an emoji is a skull-related emoji (but not jollyskull).
// Matches skull emojis (💀, ☠️) and any custom emoji with "skull" in its name.
func (b *Bot) IsSkullEmoji(emoji *discordgo.Emoji) bool {
	// Standard Unicode skull emojis
	if emoji.Name == "💀" || emoji.Name == "☠️" || emoji.Name == "☠" {
		return true
	}
	// Check for custom emojis with "skull" in the name (case-insensitive)
	name := strings.ToLower(emoji.Name)
	if !strings.Contains(name, "skull") {
		return false
	}
	// Exclude jollyskull
	if strings.Contains(name, "jollyskull") {
		return false
	}
	return true
}

// GetEmojiAPIString returns the string format needed for Discord API calls.
// For custom emojis: "name:id", for Unicode emojis: the emoji itself.
func GetEmojiAPIString(emoji *discordgo.Emoji) string {
	if emoji.ID != "" {
		return emoji.Name + ":" + emoji.ID
	}
	return emoji.Name
}
