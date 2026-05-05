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
	"github.com/u16-io/FindSenryu4Discord/pkg/msgtmpl"
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
		respondEphemeral(s, i, msgtmpl.Get("contact.guild_only", "このコマンドはサーバー内でのみ使用できます"))
		return
	}

	// サーバー管理者チェック（2層防御）
	if !isServerAdmin(i) {
		respondEphemeral(s, i, msgtmpl.Get("contact.admin_only", "このコマンドはサーバー管理者のみ使用できます"))
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
			respondEphemeral(s, i, msgtmpl.Format("contact.cooldown", "お問い合わせのクールダウン中です。あと %d分%d秒 お待ちください", minutes, seconds))
			return
		}
	}

	// Embed構築
	description := msgtmpl.Get("contact.embed_description", "お困りの内容に近いカテゴリを選択してください。\n該当するものがない場合は「その他のお問い合わせ」からメッセージを送信できます。")

	additionalMessage, err := service.GetContactAdditionalMessage()
	if err != nil {
		logger.Error("Failed to get contact additional message", "error", err)
	}
	if additionalMessage != "" {
		description += "\n\n📌 " + additionalMessage
	}

	embed := &discordgo.MessageEmbed{
		Title:       msgtmpl.Get("contact.embed_title", "お問い合わせ"),
		Description: description,
		Color:       0x5865F2,
		Footer: &discordgo.MessageEmbedFooter{
			Text: msgtmpl.Get("contact.embed_footer", "💡 Botの動作に問題がある場合は /doctor コマンドもお試しください"),
		},
	}

	// SelectMenu構築
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    ContactCategoryCustomID,
					Placeholder: msgtmpl.Get("contact.category_placeholder", "カテゴリを選択してください"),
					Options: []discordgo.SelectMenuOption{
						{
							Label: "川柳が検出されない・精度が悪い",
							Value: "faq_detection",
						},
						{
							Label: "短歌（5-7-5-7-7）は検出されますか？",
							Value: "faq_tanka",
						},
						{
							Label: "個人チャット（DM）でBotが反応しません",
							Value: "faq_dm",
						},
						{
							Label: "コマンドで「アプリケーションが応答しませんでした」と表示される",
							Value: "faq_command_error",
						},
						{
							Label: "破調（字余り・字足らず）に対応してほしい",
							Value: "faq_hachou",
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

	selected := values[0]

	switch {
	case strings.HasPrefix(selected, "faq_"):
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: msgtmpl.Get("contact.faq_prompt", "こちらのFAQページで解決策をご確認ください。"),
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

	case selected == "other":
		userID := getUserID(i)

		// クールダウンチェック
		if last, ok := contactCooldowns.Load(userID); ok {
			lastTime := last.(time.Time)
			remaining := contactCooldown - time.Since(lastTime)
			if remaining > 0 {
				minutes := int(remaining.Minutes())
				seconds := int(remaining.Seconds()) % 60
				respondEphemeral(s, i, msgtmpl.Format("contact.cooldown", "お問い合わせのクールダウン中です。あと %d分%d秒 お待ちください", minutes, seconds))
				return
			}
		}

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseModal,
			Data: &discordgo.InteractionResponseData{
				CustomID: ContactModalCustomID,
				Title:    msgtmpl.Get("contact.modal_title", "お問い合わせ"),
				Components: []discordgo.MessageComponent{
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    ContactSubjectInputID,
								Label:       msgtmpl.Get("contact.modal_subject_label", "件名"),
								Style:       discordgo.TextInputShort,
								Placeholder: msgtmpl.Get("contact.modal_subject_placeholder", "件名を入力してください"),
								Required:    true,
								MaxLength:   100,
							},
						},
					},
					discordgo.ActionsRow{
						Components: []discordgo.MessageComponent{
							discordgo.TextInput{
								CustomID:    ContactMessageInputID,
								Label:       msgtmpl.Get("contact.modal_message_label", "内容"),
								Style:       discordgo.TextInputParagraph,
								Placeholder: msgtmpl.Get("contact.modal_message_placeholder", "お問い合わせ内容を入力してください"),
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
		respondEphemeral(s, i, msgtmpl.Get("contact.send_failed", "お問い合わせの送信に失敗しました"))
		return
	}

	respondEphemeral(s, i, msgtmpl.Get("contact.send_success", "お問い合わせを送信しました ✅"))
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
			Title:    msgtmpl.Get("contact.reply_modal_title", "返信"),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    ReplyMessageInputID,
							Label:       msgtmpl.Get("contact.reply_modal_message_label", "返信内容"),
							Style:       discordgo.TextInputParagraph,
							Placeholder: msgtmpl.Get("contact.reply_modal_message_placeholder", "返信内容を入力してください"),
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
		Title:       msgtmpl.Get("contact.reply_embed_title", "お問い合わせへの返信"),
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
				Content: strPtr(msgtmpl.Get("contact.reply_dm_failed_no_channel", "DMの送信に失敗しました。送信元チャンネルの情報がないため、返信できませんでした。")),
			})
			return
		}

		_, err = s.ChannelMessageSendComplex(sourceChannelID, &discordgo.MessageSend{
			Content: msgtmpl.Format("contact.reply_channel_prefix", "<@%s> お問い合わせへの返信です:", targetUserID),
			Embeds:  []*discordgo.MessageEmbed{replyEmbed},
		})
		if err != nil {
			logger.Error("Failed to send channel reply", "error", err,
				"user_id", targetUserID, "channel_id", sourceChannelID)
			s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: strPtr(msgtmpl.Get("contact.reply_all_failed", "DMおよびチャンネルへの返信に失敗しました。")),
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
			Content: strPtr(msgtmpl.Get("contact.reply_dm_success", "DMで返信を送信しました ✅")),
		})
	} else {
		msg := msgtmpl.Format("contact.reply_channel_success", "DMの送信に失敗したため、%s に返信しました ✅", channelLabel(s, sourceChannelID))
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
		return msgtmpl.Format("contact.channel_label_fallback", "チャンネル (`%s`)", channelID)
	}
	return msgtmpl.Format("contact.channel_label", "#%s (`%s`)", ch.Name, channelID)
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
