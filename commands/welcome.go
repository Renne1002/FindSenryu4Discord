package commands

import (
	"net/http"
	"sync"

	"github.com/cockroachdb/errors"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

var welcomeSentGuilds sync.Map

// tryMarkGuildWelcomeSent atomically checks and marks a guild as welcome-sent.
// Returns true if this is the first call for the guild (i.e. welcome should be sent).
func tryMarkGuildWelcomeSent(guildID string) bool {
	_, loaded := welcomeSentGuilds.LoadOrStore(guildID, struct{}{})
	return !loaded
}

// MarkGuildWelcomeSent marks a guild as already having received the welcome message.
// Used during initial cache burst to register existing guilds.
func MarkGuildWelcomeSent(guildID string) {
	welcomeSentGuilds.Store(guildID, struct{}{})
}

// ClearGuildWelcomeSent removes a guild from the welcome-sent map.
// Called when the bot leaves a guild so that re-invitation triggers a new welcome message.
func ClearGuildWelcomeSent(guildID string) {
	welcomeSentGuilds.Delete(guildID)
}

func buildWelcomeEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "川柳検出Bot へようこそ！",
		Description: "このBotはメッセージから川柳（五・七・五）を自動検出してお知らせします。",
		Color:       0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  "川柳の検出",
				Value: "普段の会話から五・七・五のリズムを自動で見つけます。特別な操作は不要です！",
			},
			{
				Name:  "「詠め」「詠むな」",
				Value: "「詠め」と発言するとサーバー内の川柳からランダムに一句詠みます。「詠むな」で直前の自分の句を表示します。",
			},
			{
				Name:  "便利なコマンド",
				Value: "`/mute` `/unmute` — チャンネルごとの検出ON/OFF\n`/rank` — サーバー内ランキング\n`/detect off` — 自分の検出を無効化\n`/channel` — チャンネルタイプ別の設定\n`/doctor` — Bot動作の診断",
			},
			{
				Name:  "よくある質問",
				Value: "使い方やトラブルシューティングは [FAQ ページ](https://senryu-bot.u16.io/faq) をご覧ください。",
			},
		},
	}
}

// hasChannelSendPermission checks whether the bot has SendMessages permission in the given channel.
func hasChannelSendPermission(s *discordgo.Session, channelID string) bool {
	perms, err := s.State.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionSendMessages != 0
}

func resolveWelcomeChannel(s *discordgo.Session, g *discordgo.Guild) string {
	if g.SystemChannelID == "" {
		logger.Info("SystemChannelID is not set, skipping welcome message",
			"guild_id", g.ID)
		return ""
	}

	if !hasChannelSendPermission(s, g.SystemChannelID) {
		logger.Warn("SystemChannelID lacks SendMessages permission, skipping welcome message",
			"guild_id", g.ID, "system_channel_id", g.SystemChannelID)
		return ""
	}

	return g.SystemChannelID
}

// isPermanentSendError returns true if the error indicates a permanent failure
// (e.g. 403 Forbidden, 404 Not Found) where retrying would not help.
func isPermanentSendError(err error) bool {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		switch restErr.Response.StatusCode {
		case http.StatusForbidden, http.StatusNotFound:
			return true
		}
	}
	return false
}

// SendWelcomeMessage sends a welcome embed to the guild's system channel.
func SendWelcomeMessage(s *discordgo.Session, g *discordgo.GuildCreate) {
	conf := config.GetConf()
	if !conf.Discord.IsWelcomeEnabled() {
		return
	}

	if !tryMarkGuildWelcomeSent(g.ID) {
		return
	}

	channelID := resolveWelcomeChannel(s, g.Guild)
	if channelID == "" {
		logger.Warn("No writable channel found for welcome message", "guild_id", g.ID, "guild_name", g.Name)
		// Rollback so a future attempt can retry
		welcomeSentGuilds.Delete(g.ID)
		return
	}

	embed := buildWelcomeEmbed()
	if _, err := s.ChannelMessageSendEmbed(channelID, embed); err != nil {
		logger.Warn("Failed to send welcome message", "error", err, "guild_id", g.ID, "channel_id", channelID)
		// Keep the mark for permanent errors (403/404) to avoid retry storms.
		// For transient errors, rollback so a reconnect can retry.
		if !isPermanentSendError(err) {
			welcomeSentGuilds.Delete(g.ID)
		}
		return
	}

	metrics.RecordWelcomeMessageSent()
	logger.Info("Sent welcome message", "guild_id", g.ID, "guild_name", g.Name, "channel_id", channelID)
}
