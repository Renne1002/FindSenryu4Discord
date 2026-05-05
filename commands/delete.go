package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/msgtmpl"
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
		respondError(s, i, msgtmpl.Get("delete.guild_only", "このコマンドはサーバー内でのみ使用できます"))
		return
	}

	userID := getUserID(i)
	targetUserID := i.ApplicationCommandData().Options[0].UserValue(s).ID

	// 他人の川柳を削除する場合は管理者権限が必要
	if targetUserID != userID && !isServerAdmin(i) {
		respondError(s, i, msgtmpl.Get("delete.permission_denied", "他のユーザーの川柳を削除する権限がありません"))
		return
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
			Content: strPtr(msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました")),
		})
		return
	}

	if total == 0 {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(msgtmpl.Get("delete.empty", "削除できる川柳がありません")),
		})
		return
	}

	content, components := buildDeletePage(i.GuildID, targetUserID, 0, total)
	if components == nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました")),
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
		respondComponentUpdate(s, i, msgtmpl.Get("delete.invalid_operation", "無効な操作です"))
		return
	}

	guildID := parts[0]
	targetUserID := parts[1]

	// 権限チェック: ボタン押下者が元のコマンド実行者または管理者であることを確認
	if getUserID(i) != targetUserID && !isServerAdmin(i) {
		respondEphemeral(s, i, msgtmpl.Get("delete.component_permission_denied", "他のユーザーの削除操作を行う権限がありません"))
		return
	}

	page, err := strconv.Atoi(parts[2])
	if err != nil || page < 0 {
		respondComponentUpdate(s, i, msgtmpl.Get("delete.invalid_operation", "無効な操作です"))
		return
	}

	total, err := service.CountSenryusByAuthor(guildID, targetUserID)
	if err != nil {
		logger.Error("Failed to count senryus for delete page", "error", err)
		respondComponentUpdate(s, i, msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました"))
		return
	}

	if total == 0 {
		respondComponentUpdate(s, i, msgtmpl.Get("delete.empty", "削除できる川柳がありません"))
		return
	}

	content, components := buildDeletePage(guildID, targetUserID, page, total)
	if components == nil {
		respondComponentUpdate(s, i, msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました"))
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
	if total <= 0 {
		return msgtmpl.Get("delete.empty", "削除できる川柳がありません"), nil
	}

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
		return msgtmpl.Get("delete.empty", "削除できる川柳がありません"), nil
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
		content = msgtmpl.Format("delete.select_with_paging", "削除する川柳を選んでください（%d/%dページ, 全%d件）:", page+1, totalPages, total)
	} else {
		content = msgtmpl.Get("delete.select", "削除する川柳を選んでください:")
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    DeleteSelectCustomID,
					Placeholder: msgtmpl.Get("delete.select_placeholder", "川柳を選択"),
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
		respondComponentUpdate(s, i, msgtmpl.Get("delete.invalid_selection", "無効な選択です"))
		return
	}

	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, msgtmpl.Get("delete.not_found", "川柳が見つかりませんでした"))
		} else {
			respondComponentUpdate(s, i, msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました"))
		}
		return
	}

	var text string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		text = msgtmpl.Format("delete.confirm_spoiler", "||「%s %s %s」||を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		text = msgtmpl.Format("delete.confirm", "「%s %s %s」を削除しますか？", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: text,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    msgtmpl.Get("delete.confirm_button", "削除する"),
							Style:    discordgo.DangerButton,
							CustomID: DeleteConfirmPrefix + data.Values[0],
						},
						discordgo.Button{
							Label:    msgtmpl.Get("delete.cancel_button", "キャンセル"),
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
		respondComponentUpdate(s, i, msgtmpl.Get("delete.invalid_operation", "無効な操作です"))
		return
	}

	// 再度権限チェック
	senryu, err := service.GetSenryuByID(senryuID, i.GuildID)
	if err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, msgtmpl.Get("delete.not_found_maybe_deleted", "川柳が見つかりませんでした（既に削除された可能性があります）"))
		} else {
			respondComponentUpdate(s, i, msgtmpl.Get("delete.fetch_failed", "川柳の取得に失敗しました"))
		}
		return
	}

	userID := getUserID(i)
	if senryu.AuthorID != userID && !isServerAdmin(i) {
		respondComponentUpdate(s, i, msgtmpl.Get("delete.delete_permission_denied", "この川柳を削除する権限がありません"))
		return
	}

	if err := service.DeleteSenryu(senryuID, i.GuildID); err != nil {
		if errors.Is(err, service.ErrSenryuNotFound) {
			respondComponentUpdate(s, i, msgtmpl.Get("delete.not_found_maybe_deleted", "川柳が見つかりませんでした（既に削除された可能性があります）"))
		} else {
			logger.Error("Failed to delete senryu", "error", err, "id", senryuID)
			respondComponentUpdate(s, i, msgtmpl.Get("delete.delete_failed", "川柳の削除に失敗しました"))
		}
		return
	}

	var deleteText string
	if senryu.Spoiler != nil && *senryu.Spoiler {
		deleteText = msgtmpl.Format("delete.deleted_spoiler", "||「%s %s %s」||を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	} else {
		deleteText = msgtmpl.Format("delete.deleted", "「%s %s %s」を削除しました", senryu.Kamigo, senryu.Nakasichi, senryu.Simogo)
	}
	respondComponentUpdate(s, i, deleteText)
}

// HandleDeleteCancel handles the cancel button for delete
func HandleDeleteCancel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	respondComponentUpdate(s, i, msgtmpl.Get("delete.canceled", "削除をキャンセルしました"))
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
