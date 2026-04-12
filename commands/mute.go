package commands

import (
	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/service"
)

// HandleMuteCommand handles the /mute slash command.
func HandleMuteCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("mute")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	if !canManageChannel(i) {
		respondError(s, i, "このコマンドはサーバー管理者またはチャンネル管理権限を持つユーザーのみ使用できます")
		return
	}

	if err := service.ToMute(i.ChannelID, i.GuildID); err != nil {
		logger.Error("Failed to mute channel", "error", err, "channel_id", i.ChannelID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "ミュートに失敗しました",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "このチャンネルでの川柳検出をミュートしました",
			},
		})
	}
}

// HandleUnmuteCommand handles the /unmute slash command.
func HandleUnmuteCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("unmute")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	if !canManageChannel(i) {
		respondError(s, i, "このコマンドはサーバー管理者またはチャンネル管理権限を持つユーザーのみ使用できます")
		return
	}

	if err := service.ToUnMute(i.ChannelID); err != nil {
		logger.Error("Failed to unmute channel", "error", err, "channel_id", i.ChannelID)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "ミュート解除に失敗しました",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "このチャンネルでの川柳検出のミュートを解除しました",
			},
		})
	}
}
