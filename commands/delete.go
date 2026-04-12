package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/service"
)

const (
	DeleteSelectCustomID = "delete_select"
	DeleteConfirmPrefix  = "delete_confirm:"
	DeleteCancelCustomID = "delete_cancel"
	DeletePagePrefix     = "delete_page:"

	deletePageSize = 25
)

// HandleDeleteCommand handles the /delete slash command
func HandleDeleteCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	metrics.RecordCommandExecuted("delete")

	if i.GuildID == "" {
		respondError(s, i, "このコマンドはサーバー内でのみ使用できます")
		return
	}

	userID := getUserID(i)
	targetUserID := userID

	// user オプション確認
	cmdOptions := i.ApplicationCommandData().Options
	for _, opt := range cmdOptions {
		if opt.Name == "user" {
			if !isServerAdmin(i) {
				respondError(s, i, "他のユーザーの川柳を削除する権限がありません")
				return
			}
			targetUserID = opt.UserValue(s).ID
		}
	}

	// Deferred response (ephemeral)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	total, err := service.CountSenryusByAuthor(i.GuildID, targetUserID)
	if err != nil {
		logger.Error("Failed to count senryus for delete", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("川柳の取得に失敗しました"),
		})
		return
	}

	if total == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("削除できる川柳がありません"),
		})
		return
	}

	content, components := buildDeletePage(i.GuildID, targetUserID, 0, total)
	if components == nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr("川柳の取得に失敗しました"),
		})
		return
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &content,
		Components: components,
	})
}

// HandleDeletePage handles pagination button clicks for delete
func HandleDeletePage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	parts := strings.SplitN(strings.TrimPrefix(data.CustomID, DeletePagePrefix), ":", 3)
	if len(parts) != 3 {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	guildID := parts[0]
	targetUserID := parts[1]
	page, err := strconv.Atoi(parts[2])
	if err != nil || page < 0 {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	total, err := service.CountSenryusByAuthor(guildID, targetUserID)
	if err != nil {
		logger.Error("Failed to count senryus for delete page", "error", err)
		respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		return
	}

	if total == 0 {
		respondComponentUpdate(s, i, "削除できる川柳がありません")
		return
	}

	content, components := buildDeletePage(guildID, targetUserID, page, total)
	if components == nil {
		respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: componentsToSlice(components),
		},
	})
}

// buildDeletePage builds the select menu and pagination buttons for a given page.
// Returns the message content and components. components is nil on error.
func buildDeletePage(guildID, targetUserID string, page, total int) (string, *[]discordgo.MessageComponent) {
	totalPages := (total + deletePageSize - 1) / deletePageSize
	if page >= totalPages {
		page = totalPages - 1
	}

	offset := page * deletePageSize
	senryus, err := service.GetSenryusByAuthorPaged(guildID, targetUserID, deletePageSize, offset)
	if err != nil {
		logger.Error("Failed to get senryus for delete page", "error", err)
		return "", nil
	}

	if len(senryus) == 0 {
		return "削除できる川柳がありません", nil
	}

	menuOptions := make([]discordgo.SelectMenuOption, 0, len(senryus))
	for _, sr := range senryus {
		text := fmt.Sprintf("%s %s %s", sr.Kamigo, sr.Nakasichi, sr.Simogo)
		if sr.Spoiler != nil && *sr.Spoiler {
			text = "🔒 " + text
		}
		menuOptions = append(menuOptions, discordgo.SelectMenuOption{
			Label: truncateLabel(text),
			Value: strconv.Itoa(sr.ID),
		})
	}

	var content string
	if totalPages > 1 {
		content = fmt.Sprintf("削除する川柳を選んでください（%d/%dページ, 全%d件）:", page+1, totalPages, total)
	} else {
		content = "削除する川柳を選んでください:"
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    DeleteSelectCustomID,
					Placeholder: "川柳を選択",
					Options:     menuOptions,
				},
			},
		},
	}

	// Add pagination buttons if there are multiple pages
	if totalPages > 1 {
		prevID := fmt.Sprintf("%s%s:%s:%d", DeletePagePrefix, guildID, targetUserID, page-1)
		nextID := fmt.Sprintf("%s%s:%s:%d", DeletePagePrefix, guildID, targetUserID, page+1)

		components = append(components, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "◀ 前へ",
					Style:    discordgo.SecondaryButton,
					CustomID: prevID,
					Disabled: page == 0,
				},
				discordgo.Button{
					Label:    "次へ ▶",
					Style:    discordgo.SecondaryButton,
					CustomID: nextID,
					Disabled: page >= totalPages-1,
				},
			},
		})
	}

	return content, &components
}

// componentsToSlice converts *[]discordgo.MessageComponent to []discordgo.MessageComponent.
func componentsToSlice(c *[]discordgo.MessageComponent) []discordgo.MessageComponent {
	if c == nil {
		return nil
	}
	return *c
}

// truncateLabel truncates a label to fit Discord's 100-character limit for SelectMenuOption.
func truncateLabel(s string) string {
	r := []rune(s)
	if len(r) <= 100 {
		return s
	}
	return string(r[:97]) + "..."
}

// HandleDeleteSelectMenu handles the select menu interaction for delete
func HandleDeleteSelectMenu(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}

	senryuID, err := strconv.Atoi(data.Values[0])
	if err != nil {
		respondComponentUpdate(s, i, "無効な選択です")
		return
	}

	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした")
		} else {
			respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		}
		return
	}

	var text string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		text = fmt.Sprintf("||「%s %s %s」||を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		text = fmt.Sprintf("「%s %s %s」を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: text,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "削除する",
							Style:    discordgo.DangerButton,
							CustomID: DeleteConfirmPrefix + data.Values[0],
						},
						discordgo.Button{
							Label:    "キャンセル",
							Style:    discordgo.SecondaryButton,
							CustomID: DeleteCancelCustomID,
						},
					},
				},
			},
		},
	})
}

// HandleDeleteConfirm handles the confirm button for delete
func HandleDeleteConfirm(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	idStr := strings.TrimPrefix(data.CustomID, DeleteConfirmPrefix)

	senryuID, err := strconv.Atoi(idStr)
	if err != nil {
		respondComponentUpdate(s, i, "無効な操作です")
		return
	}

	// 再度権限チェック
	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした（既に削除された可能性があります）")
		} else {
			respondComponentUpdate(s, i, "川柳の取得に失敗しました")
		}
		return
	}

	userID := getUserID(i)
	if senryu.AuthorID != userID && !isServerAdmin(i) {
		respondComponentUpdate(s, i, "この川柳を削除する権限がありません")
		return
	}

	if err := service.DeleteSenryu(senryuID, i.GuildID); err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, "川柳が見つかりませんでした（既に削除された可能性があります）")
		} else {
			logger.Error("Failed to delete senryu", "error", err, "id", senryuID)
			respondComponentUpdate(s, i, "川柳の削除に失敗しました")
		}
		return
	}

	var deleteText string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		deleteText = fmt.Sprintf("||「%s %s %s」||を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		deleteText = fmt.Sprintf("「%s %s %s」を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}
	respondComponentUpdate(s, i, deleteText)
}

// HandleDeleteCancel handles the cancel button for delete
func HandleDeleteCancel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondComponentUpdate(s, i, "削除をキャンセルしました")
}

func respondComponentUpdate(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    message,
			Components: []discordgo.MessageComponent{},
		},
	})
}
