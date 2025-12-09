package bot

import (
	"context"
	"errors"
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

func TestBot_ShouldProcessReaction(t *testing.T) {
	b := &Bot{
		config:    &config.Config{TargetUserID: "user456"},
		channelID: "chan123",
		ready:     true,
	}

	tests := []struct {
		name     string
		reaction *discordgo.MessageReactionAdd
		expected bool
	}{
		{
			name: "processes skull from target user in target channel",
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
		config:    &config.Config{TargetUserID: "user456"},
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

func TestBot_ReplaceReaction(t *testing.T) {
	cfg := &config.Config{
		TargetUserID: "target-user",
		JollySkullID: "jollyskull:123",
	}

	t.Run("successful replacement", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{}

		result := b.ReplaceReaction(mock, "msg123")

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

	t.Run("fails on remove error", func(t *testing.T) {
		b := &Bot{config: cfg, channelID: "test-channel"}
		mock := &mockSession{removeErr: errors.New("remove failed")}

		result := b.ReplaceReaction(mock, "msg123")

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

		result := b.ReplaceReaction(mock, "msg123")

		if result {
			t.Error("ReplaceReaction() should return false on add error")
		}
	})
}

func TestBot_ProcessMessageReactions(t *testing.T) {
	cfg := &config.Config{
		TargetUserID: "target-user",
		JollySkullID: "jollyskull:123",
	}

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
	cfg := &config.Config{
		TargetUserID: "target-user",
		JollySkullID: "jollyskull:123",
	}

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
