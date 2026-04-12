package commands

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestBuildWelcomeEmbed_フィールド構成(t *testing.T) {
	embed := buildWelcomeEmbed()

	if embed.Title == "" {
		t.Error("embed title should not be empty")
	}
	if embed.Description == "" {
		t.Error("embed description should not be empty")
	}
	if embed.Color == 0 {
		t.Error("embed color should be set")
	}

	expectedFields := []string{"川柳の検出", "「詠め」「詠むな」", "便利なコマンド", "よくある質問"}
	if len(embed.Fields) != len(expectedFields) {
		t.Fatalf("expected %d fields, got %d", len(expectedFields), len(embed.Fields))
	}
	for i, name := range expectedFields {
		if embed.Fields[i].Name != name {
			t.Errorf("field[%d].Name = %q, want %q", i, embed.Fields[i].Name, name)
		}
		if embed.Fields[i].Value == "" {
			t.Errorf("field[%d].Value should not be empty", i)
		}
	}
}

func TestBuildWelcomeEmbed_コマンド一覧を含む(t *testing.T) {
	embed := buildWelcomeEmbed()

	commandsField := embed.Fields[2]
	commands := []string{"/mute", "/unmute", "/rank", "/detect off", "/channel", "/doctor"}
	for _, cmd := range commands {
		if !containsString(commandsField.Value, cmd) {
			t.Errorf("commands field should contain %q", cmd)
		}
	}
}

func TestResolveWelcomeChannel_SystemChannelIDあり_権限あり(t *testing.T) {
	guild := &discordgo.Guild{
		ID:              "guild-1",
		SystemChannelID: "system-ch-1",
	}

	s := &discordgo.Session{State: discordgo.NewState()}
	s.State.TrackChannels = true

	// Add the bot user and the channel with SendMessages permission to State
	s.State.User = &discordgo.User{ID: "bot-user"}
	s.State.GuildAdd(&discordgo.Guild{ID: "guild-1"})
	s.State.ChannelAdd(&discordgo.Channel{
		ID:                   "system-ch-1",
		GuildID:              "guild-1",
		Type:                 discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{},
	})
	// Guild @everyone role grants SendMessages
	s.State.RoleAdd("guild-1", &discordgo.Role{
		ID:          "guild-1",
		Permissions: discordgo.PermissionSendMessages,
	})
	s.State.MemberAdd(&discordgo.Member{
		User:    &discordgo.User{ID: "bot-user"},
		GuildID: "guild-1",
		Roles:   []string{},
	})

	got := resolveWelcomeChannel(s, guild)
	if got != "system-ch-1" {
		t.Errorf("resolveWelcomeChannel() = %q, want %q", got, "system-ch-1")
	}
}

func TestResolveWelcomeChannel_SystemChannelIDあり_権限なし(t *testing.T) {
	guild := &discordgo.Guild{
		ID:              "guild-perm",
		SystemChannelID: "system-ch-noperm",
	}

	s, _ := discordgo.New("Bot fake-token")
	s.State = discordgo.NewState()
	s.State.TrackChannels = true

	// Bot user exists but no guild/channel state -> permission check fails -> fallback
	s.State.User = &discordgo.User{ID: "bot-user"}

	// Without API backend, GuildChannels will also fail, so result is ""
	got := resolveWelcomeChannel(s, guild)
	if got != "" {
		t.Errorf("resolveWelcomeChannel() = %q, want empty string when no permission on system channel", got)
	}
}

func TestResolveWelcomeChannel_SystemChannelIDが空文字(t *testing.T) {
	guild := &discordgo.Guild{
		ID:              "guild-1",
		SystemChannelID: "",
	}

	// SystemChannelID empty -> no fallback, just return "".
	s, _ := discordgo.New("Bot fake-token")
	s.State = discordgo.NewState()
	s.State.TrackChannels = true

	got := resolveWelcomeChannel(s, guild)
	if got != "" {
		t.Errorf("resolveWelcomeChannel() = %q, want empty string when SystemChannelID is not set", got)
	}
}

func TestTryMarkGuildWelcomeSent_初回はtrue(t *testing.T) {
	// Clean up for test isolation
	welcomeSentGuilds.Range(func(key, value any) bool {
		welcomeSentGuilds.Delete(key)
		return true
	})

	guildID := "test-guild-dedup"

	if !tryMarkGuildWelcomeSent(guildID) {
		t.Error("first call should return true")
	}
}

func TestTryMarkGuildWelcomeSent_二回目はfalse(t *testing.T) {
	welcomeSentGuilds.Range(func(key, value any) bool {
		welcomeSentGuilds.Delete(key)
		return true
	})

	guildID := "test-guild-dedup2"

	tryMarkGuildWelcomeSent(guildID)

	if tryMarkGuildWelcomeSent(guildID) {
		t.Error("second call should return false")
	}
}

func TestTryMarkGuildWelcomeSent_異なるギルドは独立(t *testing.T) {
	welcomeSentGuilds.Range(func(key, value any) bool {
		welcomeSentGuilds.Delete(key)
		return true
	})

	if !tryMarkGuildWelcomeSent("guild-a") {
		t.Error("guild-a first call should return true")
	}
	if !tryMarkGuildWelcomeSent("guild-b") {
		t.Error("guild-b first call should return true (independent of guild-a)")
	}
	if tryMarkGuildWelcomeSent("guild-a") {
		t.Error("guild-a second call should return false")
	}
}

func TestMarkGuildWelcomeSent_登録済みギルドはtryMarkがfalse(t *testing.T) {
	welcomeSentGuilds.Range(func(key, value any) bool {
		welcomeSentGuilds.Delete(key)
		return true
	})

	guildID := "test-guild-pre-registered"

	MarkGuildWelcomeSent(guildID)

	if tryMarkGuildWelcomeSent(guildID) {
		t.Error("tryMark should return false for pre-registered guild")
	}
}

func TestClearGuildWelcomeSent_クリア後にtryMarkがtrue(t *testing.T) {
	welcomeSentGuilds.Range(func(key, value any) bool {
		welcomeSentGuilds.Delete(key)
		return true
	})

	guildID := "test-guild-clear"

	MarkGuildWelcomeSent(guildID)
	ClearGuildWelcomeSent(guildID)

	if !tryMarkGuildWelcomeSent(guildID) {
		t.Error("tryMark should return true after clear")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
