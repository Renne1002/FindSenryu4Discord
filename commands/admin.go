package commands

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/pkg/backup"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
	"github.com/u16-io/FindSenryu4Discord/pkg/msgtmpl"
	"github.com/u16-io/FindSenryu4Discord/pkg/permissions"
	"github.com/u16-io/FindSenryu4Discord/service"
)

var (
	backupManager *backup.Manager
	startTime     time.Time
	allSessions   []*discordgo.Session
)

// SetBackupManager sets the backup manager for admin commands
func SetBackupManager(m *backup.Manager) {
	backupManager = m
}

// SetStartTime sets the start time for uptime calculation
func SetStartTime(t time.Time) {
	startTime = t
}

// SetAllSessions sets all shard sessions for cross-shard guild counting
func SetAllSessions(sessions []*discordgo.Session) {
	allSessions = sessions
}

// allGuilds returns guilds from all shard sessions
func allGuilds() []*discordgo.Guild {
	var guilds []*discordgo.Guild
	for _, s := range allSessions {
		if s != nil {
			guilds = append(guilds, s.State.Guilds...)
		}
	}
	return guilds
}

// AdminCommands returns the admin slash commands
func AdminCommands() []*discordgo.ApplicationCommand {
	contactMessageMaxLength := 1000
	return []*discordgo.ApplicationCommand{
		{
			Name:        "admin",
			Description: "Bot管理者向けコマンド",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "stats",
					Description: "Bot統計情報を表示します",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "backup",
					Description: "手動バックアップを作成します",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name:        "contact-message",
					Description: "/contactコマンドに表示する追加メッセージを管理します",
					Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "set",
							Description: "追加メッセージを設定します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
							Options: []*discordgo.ApplicationCommandOption{
								{
									Name:        "message",
									Description: "表示するメッセージ",
									Type:        discordgo.ApplicationCommandOptionString,
									Required:    true,
									MaxLength:   contactMessageMaxLength,
								},
							},
						},
						{
							Name:        "clear",
							Description: "追加メッセージを削除します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
						},
						{
							Name:        "show",
							Description: "現在の追加メッセージを表示します",
							Type:        discordgo.ApplicationCommandOptionSubCommand,
						},
					},
				},
			},
		},
	}
}

// HandleAdminCommand handles admin slash commands
func HandleAdminCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check if user is an owner
	userID := getUserID(i)

	if !permissions.CheckOwnerPermission(userID, "admin_command") {
		respondError(s, i, msgtmpl.Get("admin.owner_only", "このコマンドはBot管理者のみ使用できます"))
		return
	}

	metrics.RecordCommandExecuted("admin")

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		respondError(s, i, msgtmpl.Get("admin.subcommand_required", "サブコマンドを指定してください"))
		return
	}

	switch options[0].Name {
	case "stats":
		handleStatsCommand(s, i)
	case "backup":
		handleBackupCommand(s, i)
	case "contact-message":
		handleContactMessageCommand(s, i, options[0].Options)
	}
}

func handleStatsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	dbStats := db.GetStats()
	conf := config.GetConf()

	uptime := time.Since(startTime).Round(time.Second)

	embed := &discordgo.MessageEmbed{
		Title:     msgtmpl.Get("admin.stats_title", "Bot Statistics"),
		Color:     0x00ff00,
		Timestamp: time.Now().Format(time.RFC3339),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Uptime",
				Value:  uptime.String(),
				Inline: true,
			},
			{
				Name:   "Connected Guilds",
				Value:  fmt.Sprintf("%d", len(allGuilds())),
				Inline: true,
			},
			{
				Name:   "Database Driver",
				Value:  conf.Database.Driver,
				Inline: true,
			},
			{
				Name:   "Total Senryus",
				Value:  fmt.Sprintf("%d", dbStats.SenryuCount),
				Inline: true,
			},
			{
				Name:   "Muted Channels",
				Value:  fmt.Sprintf("%d", dbStats.MutedChannelCount),
				Inline: true,
			},
			{
				Name:   "Database Connected",
				Value:  fmt.Sprintf("%v", dbStats.IsConnected),
				Inline: true,
			},
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

func handleBackupCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	conf := config.GetConf()

	if conf.Database.Driver != "sqlite3" {
		respondError(s, i, msgtmpl.Get("admin.backup_sqlite_only", "バックアップはSQLiteのみ対応しています"))
		return
	}

	if backupManager == nil {
		respondError(s, i, msgtmpl.Get("admin.backup_manager_unavailable", "バックアップマネージャーが初期化されていません"))
		return
	}

	// Defer response for long-running operation
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	if err := backupManager.CreateBackup(); err != nil {
		logger.Error("Manual backup failed", "error", err)
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(msgtmpl.Format("admin.backup_create_failed", "バックアップの作成に失敗しました: %s", err.Error())),
		})
		return
	}

	// Get backup list
	backups, err := backupManager.ListBackups()
	if err != nil {
		s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: strPtr(msgtmpl.Get("admin.backup_list_failed", "バックアップは作成されましたが、一覧の取得に失敗しました")),
		})
		return
	}

	description := "最新のバックアップ:\n"
	for idx, b := range backups {
		if idx >= 5 {
			break
		}
		description += fmt.Sprintf("- `%s` (%s)\n", b.Name, b.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	embed := &discordgo.MessageEmbed{
		Title:       msgtmpl.Get("admin.backup_created_title", "Backup Created"),
		Description: description,
		Color:       0x00ff00,
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &[]*discordgo.MessageEmbed{embed},
	})
}

func handleContactMessageCommand(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, msgtmpl.Get("admin.subcommand_required", "サブコマンドを指定してください"))
		return
	}

	switch options[0].Name {
	case "set":
		handleContactMessageSet(s, i, options[0].Options)
	case "clear":
		handleContactMessageClear(s, i)
	case "show":
		handleContactMessageShow(s, i)
	}
}

func handleContactMessageSet(s *discordgo.Session, i *discordgo.InteractionCreate, options []*discordgo.ApplicationCommandInteractionDataOption) {
	if len(options) == 0 {
		respondError(s, i, msgtmpl.Get("admin.contact_message_required", "メッセージを指定してください"))
		return
	}

	message := options[0].StringValue()
	if err := service.SetContactAdditionalMessage(message); err != nil {
		logger.Error("Failed to set contact additional message", "error", err)
		respondError(s, i, msgtmpl.Get("admin.contact_set_failed", "追加メッセージの設定に失敗しました"))
		return
	}

	respondEphemeral(s, i, msgtmpl.Get("admin.contact_set_success", "追加メッセージを設定しました ✅"))
}

func handleContactMessageClear(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if err := service.ClearContactAdditionalMessage(); err != nil {
		logger.Error("Failed to clear contact additional message", "error", err)
		respondError(s, i, msgtmpl.Get("admin.contact_clear_failed", "追加メッセージの削除に失敗しました"))
		return
	}

	respondEphemeral(s, i, msgtmpl.Get("admin.contact_clear_success", "追加メッセージを削除しました ✅"))
}

func handleContactMessageShow(s *discordgo.Session, i *discordgo.InteractionCreate) {
	message, err := service.GetContactAdditionalMessage()
	if err != nil {
		logger.Error("Failed to get contact additional message", "error", err)
		respondError(s, i, msgtmpl.Get("admin.contact_get_failed", "追加メッセージの取得に失敗しました"))
		return
	}

	if message == "" {
		respondEphemeral(s, i, msgtmpl.Get("admin.contact_not_set", "追加メッセージは設定されていません"))
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       msgtmpl.Get("admin.contact_current_title", "現在の追加メッセージ"),
		Description: message,
		Color:       0x5865F2,
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func getUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func isServerAdmin(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}
	return i.Member.Permissions&discordgo.PermissionAdministrator != 0
}

func canManageChannel(i *discordgo.InteractionCreate) bool {
	if i.Member == nil {
		return false
	}
	return i.Member.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageChannels) != 0
}

func respondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func strPtr(s string) *string {
	return &s
}
