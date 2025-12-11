package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"jolly-okurb/internal/config"
)

type mockSession struct {
	channels         []*discordgo.Channel
	messages         []*discordgo.Message
	messagePages     [][]*discordgo.Message // For paginated message fetching
	messageCalls     int                    // Track ChannelMessages calls
	reactions        map[string][]*discordgo.User
	removedReactions []reactionCall
	addedReactions   []reactionCall
	removeErr        error
	addErr           error
	messagesErr      error
}

// newTestConfig creates a config with TargetUserIDSet populated for testing.
func newTestConfig(targetUserIDs []string, jollySkullID string) *config.Config {
	set := make(map[string]struct{})
	for _, id := range targetUserIDs {
		set[id] = struct{}{}
	}
	return &config.Config{
		TargetUserIDs:   targetUserIDs,
		TargetUserIDSet: set,
		JollySkullID:    jollySkullID,
	}
}

type reactionCall struct {
	channelID string
	messageID string
	emojiID   string
	userID    string
}

func (m *mockSession) GuildChannels(guildID string, options ...discordgo.RequestOption) ([]*discordgo.Channel, error) {
	return m.channels, nil
}

func (m *mockSession) ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string, options ...discordgo.RequestOption) ([]*discordgo.Message, error) {
	if m.messagesErr != nil {
		return nil, m.messagesErr
	}
	// Support paginated message fetching
	if m.messagePages != nil {
		if m.messageCalls >= len(m.messagePages) {
			return nil, nil
		}
		page := m.messagePages[m.messageCalls]
		m.messageCalls++
		return page, nil
	}
	return m.messages, nil
}

func (m *mockSession) MessageReactions(channelID, messageID, emojiID string, limit int, beforeID, afterID string, options ...discordgo.RequestOption) ([]*discordgo.User, error) {
	if m.reactions == nil {
		return nil, nil
	}
	// Simulate pagination: only return results on first call (afterID == "")
	if afterID != "" {
		return nil, nil
	}
	return m.reactions[messageID], nil
}

func (m *mockSession) MessageReactionRemove(channelID, messageID, emojiID, userID string, options ...discordgo.RequestOption) error {
	m.removedReactions = append(m.removedReactions, reactionCall{channelID, messageID, emojiID, userID})
	return m.removeErr
}

func (m *mockSession) MessageReactionAdd(channelID, messageID, emojiID string, options ...discordgo.RequestOption) error {
	m.addedReactions = append(m.addedReactions, reactionCall{channelID, messageID, emojiID, ""})
	return m.addErr
}

func TestFindChannelByName(t *testing.T) {
	channels := []*discordgo.Channel{
		{ID: "1", Name: "general", Type: discordgo.ChannelTypeGuildText},
		{ID: "2", Name: "jollyposting", Type: discordgo.ChannelTypeGuildText},
		{ID: "3", Name: "voice-chat", Type: discordgo.ChannelTypeGuildVoice},
		{ID: "4", Name: "jollyposting", Type: discordgo.ChannelTypeGuildVoice},
	}

	tests := []struct {
		name     string
		search   string
		expected string
	}{
		{"finds existing channel", "jollyposting", "2"},
		{"finds general channel", "general", "1"},
		{"returns empty for non-existent", "nonexistent", ""},
		{"ignores voice channels", "voice-chat", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindChannelByName(channels, tt.search)
			if result != tt.expected {
				t.Errorf("FindChannelByName(%q) = %q, want %q", tt.search, result, tt.expected)
			}
		})
	}
}

func TestHasUser(t *testing.T) {
	users := []*discordgo.User{
		{ID: "user1"},
		{ID: "user2"},
		{ID: "target-user"},
	}

	tests := []struct {
		name     string
		users    []*discordgo.User
		targetID string
		expected bool
	}{
		{"finds target user", users, "target-user", true},
		{"finds first user", users, "user1", true},
		{"does not find missing user", users, "missing", false},
		{"handles empty list", []*discordgo.User{}, "any", false},
		{"handles nil list", nil, "any", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasUser(tt.users, tt.targetID)
			if result != tt.expected {
				t.Errorf("HasUser() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEmojiAPIString(t *testing.T) {
	tests := []struct {
		name     string
		emoji    *discordgo.Emoji
		expected string
	}{
		{"unicode skull emoji", &discordgo.Emoji{Name: "💀"}, "💀"},
		{"unicode thumbs up", &discordgo.Emoji{Name: "👍"}, "👍"},
		{"custom emoji with ID", &discordgo.Emoji{Name: "skull", ID: "123456"}, "skull:123456"},
		{"custom emoji with long ID", &discordgo.Emoji{Name: "deadskull", ID: "987654321"}, "deadskull:987654321"},
		{"animated custom emoji", &discordgo.Emoji{Name: "dance", ID: "555"}, "dance:555"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEmojiAPIString(tt.emoji)
			if result != tt.expected {
				t.Errorf("GetEmojiAPIString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestIsSkullCustomEmoji(t *testing.T) {
	tests := []struct {
		name     string
		emojiTag string
		expected bool
	}{
		{"standard skull", "<:skull:123>", true},
		{"deadskull", "<:deadskull:456>", true},
		{"skullface", "<:skullface:789>", true},
		{"animated skull", "<a:skull:111>", true},
		{"uppercase SKULL", "<:SKULL:222>", true},
		{"jollyskull excluded", "<:jollyskull:333>", false},
		{"JOLLYSKULL excluded", "<:JOLLYSKULL:444>", false},
		{"non-skull emoji", "<:party:555>", false},
		{"heart emoji", "<:heart:666>", false},
		{"malformed no colons", "<skull123>", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSkullCustomEmoji(tt.emojiTag)
			if result != tt.expected {
				t.Errorf("isSkullCustomEmoji(%q) = %v, want %v", tt.emojiTag, result, tt.expected)
			}
		})
	}
}

func TestFilterCustomEmojis(t *testing.T) {
	// Test with a simple filter that removes emojis containing "remove"
	removeFilter := func(tag string) bool {
		return strings.Contains(tag, "remove")
	}

	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{"no emojis", "hello world", "hello world"},
		{"keep non-matching emoji", "<:keep:123>", "<:keep:123>"},
		{"remove matching emoji", "<:remove:456>", ""},
		{"mixed content", "hello<:remove:1>world", "helloworld"},
		{"multiple emojis", "<:keep:1><:remove:2><:keep:3>", "<:keep:1><:keep:3>"},
		{"malformed no closing", "<:remove", "<:remove"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterCustomEmojis(tt.content, removeFilter)
			if result != tt.expected {
				t.Errorf("filterCustomEmojis(%q) = %q, want %q", tt.content, result, tt.expected)
			}
		})
	}
}

func TestBot_IsSkullEmoji(t *testing.T) {
	b := &Bot{config: &config.Config{}}

	tests := []struct {
		name     string
		emoji    *discordgo.Emoji
		expected bool
	}{
		{"unicode skull", &discordgo.Emoji{Name: "💀"}, true},
		{"custom skull emoji", &discordgo.Emoji{Name: "skull", ID: "123"}, true},
		{"custom deadskull emoji", &discordgo.Emoji{Name: "deadskull", ID: "456"}, true},
		{"custom skullface emoji", &discordgo.Emoji{Name: "skullface", ID: "789"}, true},
		{"custom SKULL uppercase", &discordgo.Emoji{Name: "SKULL", ID: "111"}, true},
		{"custom Skull mixed case", &discordgo.Emoji{Name: "Skull", ID: "222"}, true},
		{"jollyskull excluded", &discordgo.Emoji{Name: "jollyskull", ID: "333"}, false},
		{"JOLLYSKULL excluded", &discordgo.Emoji{Name: "JOLLYSKULL", ID: "444"}, false},
		{"thumbs up ignored", &discordgo.Emoji{Name: "👍"}, false},
		{"heart ignored", &discordgo.Emoji{Name: "❤️"}, false},
		{"custom non-skull emoji", &discordgo.Emoji{Name: "party", ID: "555"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.IsSkullEmoji(tt.emoji)
			if result != tt.expected {
				t.Errorf("IsSkullEmoji(%q) = %v, want %v", tt.emoji.Name, result, tt.expected)
			}
		})
	}
}

func TestBot_IsSkullOnlyMessage(t *testing.T) {
	b := &Bot{config: &config.Config{}}

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{"single unicode skull", "💀", true},
		{"multiple unicode skulls", "💀💀💀", true},
		{"skulls with spaces", "💀 💀 💀", true},
		{"skulls with newlines", "💀\n💀", true},
		{"custom skull emoji", "<:skull:123456>", true},
		{"custom deadskull emoji", "<:deadskull:789>", true},
		{"animated skull emoji", "<a:skull:123456>", true},
		{"mixed unicode and custom skulls", "💀<:skull:123>💀", true},
		{"jollyskull excluded", "<:jollyskull:123>", false},
		{"skull with text", "💀 lol", false},
		{"text only", "hello", false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"non-skull custom emoji", "<:party:123>", false},
		{"skull and non-skull emoji", "💀<:party:123>", false},
		{"skull custom emoji case insensitive", "<:SKULL:123>", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.IsSkullOnlyMessage(tt.content)
			if result != tt.expected {
				t.Errorf("IsSkullOnlyMessage(%q) = %v, want %v", tt.content, result, tt.expected)
			}
		})
	}
}

func TestBot_ShouldProcessReaction(t *testing.T) {
	b := &Bot{
		config:    newTestConfig([]string{"user456"}, ""),
		channelID: "chan123",
		ready:     true,
	}

	tests := []struct {
		name     string
		reaction *discordgo.MessageReactionAdd
		expected bool
	}{
		{
			name: "processes unicode skull from target user",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    "user456",
					Emoji:     discordgo.Emoji{Name: "💀"},
				},
			},
			expected: true,
		},
		{
			name: "processes custom skull emoji",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    "user456",
					Emoji:     discordgo.Emoji{Name: "deadskull", ID: "123456"},
				},
			},
			expected: true,
		},
		{
			name: "ignores jollyskull emoji",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    "user456",
					Emoji:     discordgo.Emoji{Name: "jollyskull", ID: "789"},
				},
			},
			expected: false,
		},
		{
			name: "ignores wrong channel",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "other-channel",
					UserID:    "user456",
					Emoji:     discordgo.Emoji{Name: "💀"},
				},
			},
			expected: false,
		},
		{
			name: "ignores wrong user",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    "other-user",
					Emoji:     discordgo.Emoji{Name: "💀"},
				},
			},
			expected: false,
		},
		{
			name: "ignores non-skull emoji",
			reaction: &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    "user456",
					Emoji:     discordgo.Emoji{Name: "👍"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.ShouldProcessReaction(tt.reaction)
			if result != tt.expected {
				t.Errorf("ShouldProcessReaction() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBot_ShouldProcessReaction_NotReady(t *testing.T) {
	b := &Bot{
		config:    newTestConfig([]string{"user456"}, ""),
		channelID: "chan123",
		ready:     false,
	}

	reaction := &discordgo.MessageReactionAdd{
		MessageReaction: &discordgo.MessageReaction{
			ChannelID: "chan123",
			UserID:    "user456",
			Emoji:     discordgo.Emoji{Name: "💀"},
		},
	}

	if b.ShouldProcessReaction(reaction) {
		t.Error("ShouldProcessReaction() should return false when bot is not ready")
	}
}

func TestBot_ShouldProcessReaction_MultipleTargetUsers(t *testing.T) {
	b := &Bot{
		config:    newTestConfig([]string{"user1", "user2", "user3"}, ""),
		channelID: "chan123",
		ready:     true,
	}

	tests := []struct {
		name     string
		userID   string
		expected bool
	}{
		{"processes first target user", "user1", true},
		{"processes second target user", "user2", true},
		{"processes third target user", "user3", true},
		{"ignores non-target user", "user4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reaction := &discordgo.MessageReactionAdd{
				MessageReaction: &discordgo.MessageReaction{
					ChannelID: "chan123",
					UserID:    tt.userID,
					Emoji:     discordgo.Emoji{Name: "💀"},
				},
			}
			result := b.ShouldProcessReaction(reaction)
			if result != tt.expected {
				t.Errorf("ShouldProcessReaction() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBot_ReplaceReaction(t *testing.T) {
	cfg := newTestConfig([]string{"target-user"}, "jollyskull:123")

	t.Run("successful replacement with unicode emoji", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{}
		emoji := &discordgo.Emoji{Name: "💀"}

		result := b.ReplaceReaction(mock, "msg123", "target-user", emoji)

		if !result {
			t.Error("ReplaceReaction() should return true on success")
		}
		if len(mock.removedReactions) != 1 {
			t.Errorf("expected 1 removed reaction, got %d", len(mock.removedReactions))
		}
		if len(mock.addedReactions) != 1 {
			t.Errorf("expected 1 added reaction, got %d", len(mock.addedReactions))
		}

		removed := mock.removedReactions[0]
		if removed.channelID != "test-channel" || removed.messageID != "msg123" ||
			removed.emojiID != "💀" || removed.userID != "target-user" {
			t.Errorf("unexpected removed reaction: %+v", removed)
		}

		added := mock.addedReactions[0]
		if added.channelID != "test-channel" || added.messageID != "msg123" ||
			added.emojiID != "jollyskull:123" {
			t.Errorf("unexpected added reaction: %+v", added)
		}
	})

	t.Run("successful replacement with custom emoji", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{}
		emoji := &discordgo.Emoji{Name: "deadskull", ID: "456789"}

		result := b.ReplaceReaction(mock, "msg123", "target-user", emoji)

		if !result {
			t.Error("ReplaceReaction() should return true on success")
		}

		removed := mock.removedReactions[0]
		if removed.emojiID != "deadskull:456789" {
			t.Errorf("expected custom emoji format, got %q", removed.emojiID)
		}
	})

	t.Run("fails on remove error", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{removeErr: errors.New("remove failed")}
		emoji := &discordgo.Emoji{Name: "💀"}

		result := b.ReplaceReaction(mock, "msg123", "target-user", emoji)

		if result {
			t.Error("ReplaceReaction() should return false on remove error")
		}
		if len(mock.addedReactions) != 0 {
			t.Error("should not add reaction if remove fails")
		}
	})

	t.Run("fails on add error", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{addErr: errors.New("add failed")}
		emoji := &discordgo.Emoji{Name: "💀"}

		result := b.ReplaceReaction(mock, "msg123", "target-user", emoji)

		if result {
			t.Error("ReplaceReaction() should return false on add error")
		}
	})
}

func TestBot_ProcessMessageReactions(t *testing.T) {
	cfg := newTestConfig([]string{"target-user"}, "jollyskull:123")

	t.Run("replaces skull reaction from target user", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{
			reactions: map[string][]*discordgo.User{
				"msg1": {{ID: "other-user"}, {ID: "target-user"}},
			},
		}
		msg := &discordgo.Message{
			ID: "msg1",
			Reactions: []*discordgo.MessageReactions{
				{Emoji: &discordgo.Emoji{Name: "💀"}},
			},
		}

		count := b.ProcessMessageReactions(mock, msg)

		if count != 1 {
			t.Errorf("expected 1 replacement, got %d", count)
		}
	})

	t.Run("ignores non-skull reactions", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{
			reactions: map[string][]*discordgo.User{
				"msg1": {{ID: "target-user"}},
			},
		}
		msg := &discordgo.Message{
			ID: "msg1",
			Reactions: []*discordgo.MessageReactions{
				{Emoji: &discordgo.Emoji{Name: "👍"}},
			},
		}

		count := b.ProcessMessageReactions(mock, msg)

		if count != 0 {
			t.Errorf("expected 0 replacements, got %d", count)
		}
	})

	t.Run("ignores skull reactions from other users", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{
			reactions: map[string][]*discordgo.User{
				"msg1": {{ID: "other-user1"}, {ID: "other-user2"}},
			},
		}
		msg := &discordgo.Message{
			ID: "msg1",
			Reactions: []*discordgo.MessageReactions{
				{Emoji: &discordgo.Emoji{Name: "💀"}},
			},
		}

		count := b.ProcessMessageReactions(mock, msg)

		if count != 0 {
			t.Errorf("expected 0 replacements, got %d", count)
		}
	})

	t.Run("handles message with no reactions", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{}
		msg := &discordgo.Message{ID: "msg1", Reactions: nil}

		count := b.ProcessMessageReactions(mock, msg)

		if count != 0 {
			t.Errorf("expected 0 replacements, got %d", count)
		}
	})
}

func TestBot_Initialize(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		cfg := &config.Config{
			GuildID:     "guild123",
			ChannelName: "jollyposting",
		}
		b := New(cfg)
		mock := &mockSession{
			channels: []*discordgo.Channel{
				{ID: "chan1", Name: "general", Type: discordgo.ChannelTypeGuildText},
				{ID: "chan2", Name: "jollyposting", Type: discordgo.ChannelTypeGuildText},
			},
		}

		err := b.Initialize(mock)

		if err != nil {
			t.Errorf("Initialize() unexpected error: %v", err)
		}
		if b.channelID != "chan2" {
			t.Errorf("channelID = %q, want %q", b.channelID, "chan2")
		}
		if !b.ready {
			t.Error("bot should be ready after initialization")
		}
	})

	t.Run("channel not found", func(t *testing.T) {
		cfg := &config.Config{
			GuildID:     "guild123",
			ChannelName: "nonexistent",
		}
		b := New(cfg)
		mock := &mockSession{
			channels: []*discordgo.Channel{
				{ID: "chan1", Name: "general", Type: discordgo.ChannelTypeGuildText},
			},
		}

		err := b.Initialize(mock)

		if err == nil {
			t.Error("Initialize() should return error when channel not found")
		}
	})
}

func TestBot_ProcessHistoricalMessages(t *testing.T) {
	cfg := newTestConfig([]string{"target-user"}, "jollyskull:123")

	t.Run("processes messages until cutoff", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}

		// Create messages: one after cutoff, one before
		afterCutoff := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		beforeCutoff := time.Date(2024, 12, 15, 12, 0, 0, 0, time.UTC)

		mock := &mockSession{
			messagePages: [][]*discordgo.Message{
				{
					{ID: "msg1", Timestamp: afterCutoff, Reactions: nil},
					{ID: "msg2", Timestamp: beforeCutoff, Reactions: nil},
				},
			},
		}

		ctx := context.Background()
		b.ProcessHistoricalMessages(ctx, mock)

		if mock.messageCalls != 1 {
			t.Errorf("expected 1 message fetch call, got %d", mock.messageCalls)
		}
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		mock := &mockSession{
			messagePages: [][]*discordgo.Message{
				{{ID: "msg1", Timestamp: time.Now()}},
			},
		}

		b.ProcessHistoricalMessages(ctx, mock)

		// Should exit immediately without processing
		if mock.messageCalls != 0 {
			t.Errorf("expected 0 message fetch calls after cancel, got %d", mock.messageCalls)
		}
	})

	t.Run("handles empty channel", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{
			messagePages: [][]*discordgo.Message{
				{}, // Empty first page
			},
		}

		ctx := context.Background()
		b.ProcessHistoricalMessages(ctx, mock)

		if mock.messageCalls != 1 {
			t.Errorf("expected 1 message fetch call, got %d", mock.messageCalls)
		}
	})

	t.Run("handles fetch error", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{
			messagesErr: errors.New("API error"),
		}

		ctx := context.Background()
		b.ProcessHistoricalMessages(ctx, mock)

		// Should exit gracefully on error
	})

	t.Run("replaces reactions during historical processing", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}

		afterCutoff := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
		beforeCutoff := time.Date(2024, 12, 15, 12, 0, 0, 0, time.UTC)

		mock := &mockSession{
			messagePages: [][]*discordgo.Message{
				{
					{
						ID:        "msg1",
						Timestamp: afterCutoff,
						Reactions: []*discordgo.MessageReactions{
							{Emoji: &discordgo.Emoji{Name: "💀"}},
						},
					},
					{ID: "msg2", Timestamp: beforeCutoff},
				},
			},
			reactions: map[string][]*discordgo.User{
				"msg1": {{ID: "target-user"}},
			},
		}

		ctx := context.Background()
		b.ProcessHistoricalMessages(ctx, mock)

		if len(mock.removedReactions) != 1 {
			t.Errorf("expected 1 removed reaction, got %d", len(mock.removedReactions))
		}
		if len(mock.addedReactions) != 1 {
			t.Errorf("expected 1 added reaction, got %d", len(mock.addedReactions))
		}
	})
}

func TestBot_ShouldDeleteMessage(t *testing.T) {
	b := &Bot{
		config:    newTestConfig([]string{"user456"}, ""),
		channelID: "chan123",
		ready:     true,
	}

	tests := []struct {
		name     string
		message  *discordgo.MessageCreate
		expected bool
	}{
		{
			name: "deletes skull-only message from target user",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "deletes skull with whitespace",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "  💀  ",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "deletes multiple skulls",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀💀💀",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "deletes multiple skulls with spaces",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀 💀 💀",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "deletes custom skull emoji message",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "<:skull:123456>",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "deletes mixed skull emojis",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀<:deadskull:789>💀",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: true,
		},
		{
			name: "ignores jollyskull-only message",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "<:jollyskull:123>",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
		{
			name: "ignores skull with other text",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀 lol",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
		{
			name: "ignores non-skull message",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "hello",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
		{
			name: "ignores wrong channel",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "other-channel",
					Content:   "💀",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
		{
			name: "ignores wrong user",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀",
					Author:    &discordgo.User{ID: "other-user"},
				},
			},
			expected: false,
		},
		{
			name: "ignores nil author",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "💀",
					Author:    nil,
				},
			},
			expected: false,
		},
		{
			name: "ignores empty message",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
		{
			name: "ignores whitespace-only message",
			message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: "chan123",
					Content:   "   ",
					Author:    &discordgo.User{ID: "user456"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.ShouldDeleteMessage(tt.message)
			if result != tt.expected {
				t.Errorf("ShouldDeleteMessage() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBot_ShouldDeleteMessage_NotReady(t *testing.T) {
	b := &Bot{
		config:    newTestConfig([]string{"user456"}, ""),
		channelID: "chan123",
		ready:     false,
	}

	message := &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID: "chan123",
			Content:   "💀",
			Author:    &discordgo.User{ID: "user456"},
		},
	}

	if b.ShouldDeleteMessage(message) {
		t.Error("ShouldDeleteMessage() should return false when bot is not ready")
	}
}

func TestBot_Shutdown(t *testing.T) {
	t.Run("cancels context", func(t *testing.T) {
		b := New(&config.Config{})
		ctx, cancel := context.WithCancel(context.Background())
		b.cancel = cancel

		b.Shutdown()

		select {
		case <-ctx.Done():
			// Context was cancelled as expected
		case <-time.After(100 * time.Millisecond):
			t.Error("Shutdown() should cancel the context")
		}
	})

	t.Run("handles nil cancel", func(t *testing.T) {
		b := New(&config.Config{})
		// cancel is nil by default

		// Should not panic
		b.Shutdown()
	})
}
