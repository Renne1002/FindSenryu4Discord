package commands

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/msgtmpl"
	"github.com/u16-io/FindSenryu4Discord/service"
)

// requiredPermission represents a Discord permission required by the bot
type requiredPermission struct {
	Name     string
	Flag     int64
	Required bool // true=必須, false=推奨
	Reason   string
}

var requiredPermissions = []requiredPermission{
	{Name: "チャンネルの閲覧", Flag: discordgo.PermissionViewChannel, Required: true, Reason: "メッセージを受信するために必要です"},
	{Name: "メッセージの送信", Flag: discordgo.PermissionSendMessages, Required: true, Reason: "川柳検出の返信に必要です"},
	{Name: "メッセージ履歴の閲覧", Flag: discordgo.PermissionReadMessageHistory, Required: true, Reason: "メッセージへの返信に必要です"},
	{Name: "埋め込みリンク", Flag: discordgo.PermissionEmbedLinks, Required: false, Reason: "ランキング表示に必要です"},
	{Name: "外部の絵文字の使用", Flag: discordgo.PermissionUseExternalEmojis, Required: false, Reason: "ランキングの装飾に使用します"},
}

// HandleDoctorCommand handles the /doctor slash command
func HandleDoctorCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("doctor")

	if i.GuildID == "" {
		respondError(s, i, msgtmpl.Get("doctor.guild_only", "このコマンドはサーバー内でのみ使用できます"))
		return
	}

	// Get bot's permissions in this channel
	perms, err := s.State.UserChannelPermissions(s.State.User.ID, i.ChannelID)
	if err != nil {
		logger.Warn("Failed to get bot permissions", "error", err, "channel_id", i.ChannelID)
		respondError(s, i, msgtmpl.Get("doctor.permission_fetch_failed", "権限情報の取得に失敗しました"))
		return
	}

	// Check channel type
	ch, err := s.State.Channel(i.ChannelID)
	if err != nil {
		ch, err = s.Channel(i.ChannelID)
		if err != nil {
			logger.Warn("Failed to get channel", "error", err, "channel_id", i.ChannelID)
			respondError(s, i, msgtmpl.Get("doctor.channel_fetch_failed", "チャンネル情報の取得に失敗しました"))
			return
		}
	}

	// Build diagnosis results
	var results []string
	hasError := false

	// 1. Permission checks
	for _, p := range requiredPermissions {
		has := perms&p.Flag != 0
		if has {
			results = append(results, fmt.Sprintf("✅ %s", p.Name))
		} else if p.Required {
			results = append(results, fmt.Sprintf("❌ %s — %s", p.Name, p.Reason))
			hasError = true
		} else {
			results = append(results, fmt.Sprintf("⚠️ %s — %s", p.Name, p.Reason))
		}
	}

	// 2. Channel type check
	channelTypeEnabled := service.IsChannelTypeEnabled(i.GuildID, ch.Type)
	channelTypeName := channelTypeName(ch.Type)
	if channelTypeEnabled {
		results = append(results, fmt.Sprintf("✅ チャンネルタイプ「%s」は検出対象です", channelTypeName))
	} else {
		results = append(results, fmt.Sprintf("❌ チャンネルタイプ「%s」は検出対象外です — `/channel` で設定を変更できます", channelTypeName))
		hasError = true
	}

	// 3. Mute check
	isMuted := service.IsMute(i.ChannelID)
	if isMuted {
		results = append(results, "❌ このチャンネルはミュートされています — `/unmute` で解除できます")
		hasError = true
	} else {
		results = append(results, "✅ このチャンネルはミュートされていません")
	}

	// 4. Parent channel mute check (for threads)
	if ch.ParentID != "" {
		parentMuted := service.IsMute(ch.ParentID)
		if parentMuted {
			results = append(results, "❌ 親チャンネルがミュートされています — 親チャンネルで `/unmute` を実行してください")
			hasError = true
		} else {
			results = append(results, "✅ 親チャンネルはミュートされていません")
		}
	}

	// 5. User opt-out check
	userID := getUserID(i)
	if service.IsDetectionOptedOut(i.GuildID, userID) {
		results = append(results, "⚠️ あなたは川柳検出を無効にしています — `/detect on` で有効にできます")
	} else {
		results = append(results, "✅ あなたの川柳検出は有効です")
	}

	// Build embed
	var status string
	var color int
	if hasError {
		status = msgtmpl.Get("doctor.status_error", "問題が見つかりました")
		color = 0xff0000
	} else {
		status = msgtmpl.Get("doctor.status_ok", "問題ありません")
		color = 0x00ff00
	}

	embed := &discordgo.MessageEmbed{
		Title:       msgtmpl.Format("doctor.title", "診断結果: %s", status),
		Description: strings.Join(results, "\n"),
		Color:       color,
		Footer: &discordgo.MessageEmbedFooter{
			Text: msgtmpl.Format("doctor.footer", "#%s", channelDisplayName(ch)),
		},
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func channelTypeName(t discordgo.ChannelType) string {
	switch t {
	case discordgo.ChannelTypeGuildText:
		return "テキストチャンネル"
	case discordgo.ChannelTypeGuildVoice:
		return "ボイスチャンネル"
	case discordgo.ChannelTypeGuildNews:
		return "アナウンスチャンネル"
	case discordgo.ChannelTypeGuildNewsThread:
		return "アナウンススレッド"
	case discordgo.ChannelTypeGuildPublicThread:
		return "公開スレッド"
	case discordgo.ChannelTypeGuildPrivateThread:
		return "プライベートスレッド"
	case discordgo.ChannelTypeGuildStageVoice:
		return "ステージチャンネル"
	case discordgo.ChannelTypeGuildForum:
		return "フォーラムチャンネル"
	default:
		return fmt.Sprintf("不明 (%d)", t)
	}
}

func channelDisplayName(ch *discordgo.Channel) string {
	if ch.Name != "" {
		return ch.Name
	}
	return ch.ID
}
