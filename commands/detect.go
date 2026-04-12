package commands

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/service"
)

// HandleDetectCommand handles the /detect slash command
func HandleDetectCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("detect")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	userID := getUserID(i)
	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, "サブコマンドを指定してください")
		return
	}

	subCmd := options[0].Name

	switch subCmd {
	case "on":
		if err := service.OptInDetection(i.GuildID, userID, false); err != nil {
			if errors.Is(err, service.ErrAdminBanned) {
				respondEphemeral(s, i, "管理者によって川柳検出が無効化されています。解除するにはサーバー管理者に連絡してください。")
				return
			}
			logger.Error("Failed to opt in detection", "error", err, "user_id", userID, "guild_id", i.GuildID)
			respondEphemeral(s, i, "川柳検出の有効化に失敗しました")
			return
		}
		respondEphemeral(s, i, "川柳検出を有効にしました ✅")

	case "off":
		if err := service.OptOutDetection(i.GuildID, userID, "self"); err != nil {
			logger.Error("Failed to opt out detection", "error", err, "user_id", userID, "guild_id", i.GuildID)
			respondEphemeral(s, i, "川柳検出の無効化に失敗しました")
			return
		}
		respondEphemeral(s, i, "川柳検出を無効にしました ✅")

	case "status":
		if service.IsDetectionOptedOut(i.GuildID, userID) {
			respondEphemeral(s, i, "現在の設定: 川柳検出 **無効**")
		} else {
			respondEphemeral(s, i, "現在の設定: 川柳検出 **有効**")
		}

	case "ban":
		handleDetectBan(s, i)

	case "unban":
		handleDetectUnban(s, i)

	case "list":
		handleDetectList(s, i)
	}
}

func handleDetectBan(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !isServerAdmin(i) {
		respondEphemeral(s, i, "このコマンドはサーバー管理者のみ使用できます")
		return
	}

	targetUser := i.ApplicationCommandData().Options[0].Options[0].UserValue(s)
	if targetUser == nil {
		respondEphemeral(s, i, "ユーザーの取得に失敗しました")
		return
	}

	if targetUser.Bot {
		respondEphemeral(s, i, "Botユーザーをbanすることはできません")
		return
	}

	if err := service.AdminBanDetection(i.GuildID, targetUser.ID); err != nil {
		logger.Error("Failed to admin ban user", "error", err, "target_user_id", targetUser.ID, "guild_id", i.GuildID)
		respondEphemeral(s, i, "ユーザーのbanに失敗しました")
		return
	}

	respondEphemeral(s, i, fmt.Sprintf("<@%s> の川柳検出を無効化しました ✅", targetUser.ID))
}

func handleDetectUnban(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !isServerAdmin(i) {
		respondEphemeral(s, i, "このコマンドはサーバー管理者のみ使用できます")
		return
	}

	targetUser := i.ApplicationCommandData().Options[0].Options[0].UserValue(s)
	if targetUser == nil {
		respondEphemeral(s, i, "ユーザーの取得に失敗しました")
		return
	}

	if err := service.OptInDetection(i.GuildID, targetUser.ID, true); err != nil {
		logger.Error("Failed to admin unban user", "error", err, "target_user_id", targetUser.ID, "guild_id", i.GuildID)
		respondEphemeral(s, i, "ユーザーのunbanに失敗しました")
		return
	}

	respondEphemeral(s, i, fmt.Sprintf("<@%s> の川柳検出無効化を解除しました ✅", targetUser.ID))
}

func handleDetectList(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !isServerAdmin(i) {
		respondEphemeral(s, i, "このコマンドはサーバー管理者のみ使用できます")
		return
	}

	optOuts, err := service.ListOptOutsByServer(i.GuildID)
	if err != nil {
		logger.Error("Failed to list opt-outs", "error", err, "guild_id", i.GuildID)
		respondEphemeral(s, i, "一覧の取得に失敗しました")
		return
	}

	if len(optOuts) == 0 {
		respondEphemeral(s, i, "川柳検出を無効化しているユーザーはいません")
		return
	}

	var lines []string
	for idx, o := range optOuts {
		if idx >= 25 {
			lines = append(lines, fmt.Sprintf("...他 %d 件", len(optOuts)-25))
			break
		}
		setByLabel := "自己設定"
		if o.SetBy == "admin" {
			setByLabel = "管理者BAN"
		}
		lines = append(lines, fmt.Sprintf("- <@%s> (%s)", o.UserID, setByLabel))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "川柳検出無効化ユーザー一覧",
		Description: strings.Join(lines, "\n"),
		Color:       0xFF9900,
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}
