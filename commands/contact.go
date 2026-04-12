package commands

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/service"
)

const (
	ContactModalCustomID    = "contact_modal"
	ContactSubjectInputID   = "contact_subject"
	ContactMessageInputID   = "contact_message"
	ContactReplyPrefix      = "contact_reply:"
	ReplyModalPrefix        = "reply_modal:"
	ReplyMessageInputID     = "reply_message"
	ContactCategoryCustomID = "contact_category"
	contactCooldown         = 5 * time.Minute
	contactFaqURL           = "https://senryu-bot.u16.io/faq"
)

var contactCooldowns sync.Map // userID -> time.Time

// HandleContactCommand handles the /contact slash command.
// Shows an ephemeral guide message with a category SelectMenu.
func HandleContactCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("contact")

	// DM防止
	if i.GuildID == "" {
		respondEphemeral(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	// サーバー管理者チェック（2層防御）
	if !isServerAdmin(i) {
		respondEphemeral(s, i, "このコマンドはサーバー管理者のみ使用できます")
		return
	}

	userID := getUserID(i)

	// クールダウンチェック
	if last, ok := contactCooldowns.Load(userID); ok {
		lastTime := last.(time.Time)
		remaining := contactCooldown - time.Since(lastTime)
		if remaining > 0 {
			minutes := int(remaining.Minutes())
			seconds := int(remaining.Seconds()) % 60
			respondEphemeral(s, i, fmt.Sprintf("お問い合わせのクールダウン中です。あと %d分%d秒 お待ちください", minutes, seconds))
			return
		}
	}

	// Embed構築
	description := "川柳が検出されない・Botが動かないなどの問題は、まず `/doctor` コマンドで診断をお試しください。\n権限やチャンネル設定の問題を自動で確認できます。"

	additionalMessage, err := service.GetContactAdditionalMessage()
	if err != nil {
		logger.Error("Failed to get contact additional message", "error", err)
	}
	if additionalMessage != "" {
		description += "\n\n" + additionalMessage
	}

	embed := &discordgo.MessageEmbed{
		Title:       "お問い合わせ",
		Description: description,
		Color:       0x5865F2,
	}

	// SelectMenu構築
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    ContactCategoryCustomID,
					Placeholder: "カテゴリを選択してください",
					Options: []discordgo.SelectMenuOption{
						{
							Label:       "よくある質問",
							Value:       "faq",
							Description: "FAQページで解決策を確認する",
						},
						{
							Label:       "その他のお問い合わせ",
							Value:       "other",
							Description: "Bot管理者にメッセージを送信する",
						},
					},
				},
			},
		},
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
			Flags:      discordgo.MessageFlagsEphemeral,
		},
	})
}

// HandleContactCategorySelect handles the category SelectMenu interaction.
func HandleContactCategorySelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}

	switch values[0] {
	case "faq":
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "よくある質問はこちらをご確認ください。",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.Button{
								Label: "よくある質問 (FAQ)",
								Style: discordgo.LinkButton,
								URL:   contactFaqURL,
							},
						},
					},
				},
				Flags: discordgo.MessageFlagsEphemeral,
			},
		})

	case "other":
		userID := getUserID(i)

		// クールダウンチェック
		if last, ok := contactCooldowns.Load(userID); ok {
			lastTime := last.(time.Time)
			remaining := contactCooldown - time.Since(lastTime)
			if remaining > 0 {
				minutes := int(remaining.Minutes())
				seconds := int(remaining.Seconds()) % 60
				respondEphemeral(s, i, fmt.Sprintf("お問い合わせのクールダウン中です。あと %d分%d秒 お待ちください", minutes, seconds))
				return
			}
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: ContactModalCustomID,
				Title:    "お問い合わせ",
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    ContactSubjectInputID,
								Label:       "件名",
								Style:       discordgo.TextInputShort,
								Placeholder: "件名を入力してください",
								Required:    true,
								MaxLength:   100,
							},
						},
					},
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    ContactMessageInputID,
								Label:       "内容",
								Style:       discordgo.TextInputParagraph,
								Placeholder: "お問い合わせ内容を入力してください",
								Required:    true,
								MaxLength:   2000,
							},
						},
					},
				},
			},
		})
	}
}

// HandleContactModalSubmit handles the contact modal submission
func HandleContactModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	subject := getModalInputValue(data, ContactSubjectInputID)
	message := getModalInputValue(data, ContactMessageInputID)

	userID := getUserID(i)

	// クールダウン記録
	contactCooldowns.Store(userID, time.Now())

	// ユーザー情報取得
	var userName, avatarURL string
	if i.Member != nil {
		userName = i.Member.User.Username
		avatarURL = i.Member.User.AvatarURL("")
	} else if i.User != nil {
		userName = i.User.Username
		avatarURL = i.User.AvatarURL("")
	}

	// Embed構築
	embed := &discordgo.MessageEmbed{
		Title:       subject,
		Description: message,
		Color:       0x5865F2,
		Author: &discordgo.MessageEmbedAuthor{
			Name:    userName,
			IconURL: avatarURL,
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Contact Form",
		},
	}

	// Fields
	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "ユーザー",
			Value:  fmt.Sprintf("<@%s> (`%s`)", userID, userID),
			Inline: true,
		},
	}

	if i.GuildID != "" {
		guildName := "Unknown"
		guild, err := s.Guild(i.GuildID)
		if err == nil {
			guildName = guild.Name
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "サーバー",
			Value:  fmt.Sprintf("%s (`%s`)", guildName, i.GuildID),
			Inline: true,
		})
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "チャンネル",
			Value:  fmt.Sprintf("<#%s> (`%s`)", i.ChannelID, i.ChannelID),
			Inline: true,
		})
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "サーバー",
			Value:  "DM",
			Inline: true,
		})
	}

	embed.Fields = fields

	// contact_channel_id に送信
	conf := config.GetConf()
	contactChannelID := conf.Admin.ContactChannelID

	// customID に送信元チャンネルIDも埋め込む (contact_reply:userID:channelID)
	replyTarget := userID + ":" + i.ChannelID
	_, err := s.ChannelMessageSendComplex(contactChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "返信",
						Style:    discordgo.PrimaryButton,
						CustomID: ContactReplyPrefix + replyTarget,
					},
				},
			},
		},
	})
	if err != nil {
		logger.Error("Failed to send contact message", "error", err, "channel_id", contactChannelID)
		respondEphemeral(s, i, "お問い合わせの送信に失敗しました")
		return
	}

	respondEphemeral(s, i, "お問い合わせを送信しました ✅")
}

// parseReplyTarget parses "userID:channelID" from customID payload.
// Falls back to treating the entire string as userID for backwards compatibility.
func parseReplyTarget(payload string) (userID, channelID string) {
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return payload, ""
}

// HandleContactReplyButton handles the reply button click on contact messages
func HandleContactReplyButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	payload := strings.TrimPrefix(i.MessageComponentData().CustomID, ContactReplyPrefix)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: ReplyModalPrefix + payload,
			Title:    "返信",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    ReplyMessageInputID,
							Label:       "返信内容",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "返信内容を入力してください",
							Required:    true,
							MaxLength:   2000,
						},
					},
				},
			},
		},
	})
}

// HandleContactReplyModalSubmit handles the reply modal submission
func HandleContactReplyModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// DM送信やチャンネル送信に時間がかかる場合があるため、先にDeferする
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	data := i.ModalSubmitData()
	payload := strings.TrimPrefix(data.CustomID, ReplyModalPrefix)
	targetUserID, sourceChannelID := parseReplyTarget(payload)
	replyMessage := getModalInputValue(data, ReplyMessageInputID)

	replyEmbed := &discordgo.MessageEmbed{
		Title:       "お問い合わせへの返信",
		Description: replyMessage,
		Color:       0x5865F2,
		Author: &discordgo.MessageEmbedAuthor{
			Name:    s.State.User.Username,
			IconURL: s.State.User.AvatarURL(""),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// まずDMで返信を試みる
	sentViaDM := false
	dmChannel, err := s.UserChannelCreate(targetUserID)
	if err == nil {
		_, err = s.ChannelMessageSendEmbed(dmChannel.ID, replyEmbed)
		if err == nil {
			sentViaDM = true
		}
	}

	// DM失敗時、送信元チャンネルにメンション付きで返信
	if !sentViaDM {
		logger.Warn("DM reply failed, falling back to channel reply",
			"error", err, "user_id", targetUserID, "channel_id", sourceChannelID)

		if sourceChannelID == "" {
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: strPtr("DMの送信に失敗しました。送信元チャンネルの情報がないため、返信できませんでした。"),
			})
			return
		}

		_, err = s.ChannelMessageSendComplex(sourceChannelID, &discordgo.MessageSend{
			Content: fmt.Sprintf("<@%s> お問い合わせへの返信です:", targetUserID),
			Embeds:  []*discordgo.MessageEmbed{replyEmbed},
		})
		if err != nil {
			logger.Error("Failed to send channel reply", "error", err,
				"user_id", targetUserID, "channel_id", sourceChannelID)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: strPtr("DMおよびチャンネルへの返信に失敗しました。"),
			})
			return
		}
	}

	// 送信成功後のみ、元メッセージを「返信済み」に更新
	if i.Message != nil {
		var replierName string
		if i.Member != nil {
			replierName = i.Member.User.Username
		} else if i.User != nil {
			replierName = i.User.Username
		}

		replyMethod := "DM"
		if !sentViaDM {
			replyMethod = channelLabel(s, sourceChannelID)
		}

		embeds := i.Message.Embeds
		if len(embeds) > 0 {
			embeds[0].Fields = append(embeds[0].Fields, &discordgo.MessageEmbedField{
				Name:   "返信済み",
				Value:  fmt.Sprintf("%s が %s で返信しました", replierName, replyMethod),
				Inline: false,
			})
		}

		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel: i.ChannelID,
			ID:      i.Message.ID,
			Embeds:  &embeds,
			Components: &[]discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "返信済み",
							Style:    discordgo.SecondaryButton,
							CustomID: ContactReplyPrefix + payload,
							Disabled: true,
						},
					},
				},
			},
		})
	}

	if sentViaDM {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("DMで返信を送信しました ✅"),
		})
	} else {
		msg := fmt.Sprintf("DMの送信に失敗したため、%s に返信しました ✅", channelLabel(s, sourceChannelID))
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(msg),
		})
	}
}

// channelLabel returns a human-readable channel name with ID.
// Bot can resolve the channel name via API even if the admin user cannot see it.
func channelLabel(s *discordgo.Session, channelID string) string {
	ch, err := s.Channel(channelID)
	if err != nil {
		return fmt.Sprintf("チャンネル (`%s`)", channelID)
	}
	return fmt.Sprintf("#%s (`%s`)", ch.Name, channelID)
}

// getModalInputValue extracts a text input value from modal submit data
func getModalInputValue(data discordgo.ModalSubmitInteractionData, customID string) string {
	for _, comp := range data.Components {
		row, ok := comp.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, rowComp := range row.Components {
			input, ok := rowComp.(*discordgo.TextInput)
			if !ok {
				continue
			}
			if input.CustomID == customID {
				return input.Value
			}
		}
	}
	return ""
}
