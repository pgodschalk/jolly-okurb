package bot

import "github.com/bwmarrin/discordgo"

// Session abstracts the Discord API for testing.
type Session interface {
	GuildChannels(guildID string, options ...discordgo.RequestOption) ([]*discordgo.Channel, error)
	ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string, options ...discordgo.RequestOption) ([]*discordgo.Message, error)
	MessageReactions(channelID, messageID, emojiID string, limit int, beforeID, afterID string, options ...discordgo.RequestOption) ([]*discordgo.User, error)
	MessageReactionRemove(channelID, messageID, emojiID, userID string, options ...discordgo.RequestOption) error
	MessageReactionAdd(channelID, messageID, emojiID string, options ...discordgo.RequestOption) error
}
