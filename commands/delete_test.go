package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
)

func setupDeleteTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.Senryu{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func seedDeleteSenryus(t *testing.T, serverID, authorID string, count int) {
	t.Helper()
	f := false
	for i := 0; i < count; i++ {
		s := model.Senryu{
			ServerID:  serverID,
			AuthorID:  authorID,
			Kamigo:    fmt.Sprintf("上の句%d", i+1),
			Nakasichi: fmt.Sprintf("中の句%d", i+1),
			Simogo:    fmt.Sprintf("下の句%d", i+1),
			Spoiler:   &f,
		}
		if err := db.DB.Create(&s).Error; err != nil {
			t.Fatalf("failed to seed senryu: %v", err)
		}
	}
}

func TestTruncateLabel_100文字以下はそのまま(t *testing.T) {
	s := strings.Repeat("あ", 100)
	got := truncateLabel(s)
	if got != s {
		t.Errorf("expected no truncation for 100-char string, got len=%d", len([]rune(got)))
	}
}

func TestTruncateLabel_101文字以上は切り詰め(t *testing.T) {
	s := strings.Repeat("あ", 101)
	got := truncateLabel(s)
	r := []rune(got)
	if len(r) != 100 {
		t.Errorf("expected 100 runes, got %d", len(r))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected suffix '...', got %q", got[len(got)-9:])
	}
}

func TestTruncateLabel_空文字列(t *testing.T) {
	got := truncateLabel("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBuildDeletePage_1ページに収まる(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 5)

	content, components := buildDeletePage("guild1", "user1", 0, 5)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	if strings.Contains(content, "ページ") {
		t.Error("should not contain page info for single page")
	}

	// 1 ActionRow (select menu only, no pagination buttons)
	if len(*components) != 1 {
		t.Errorf("expected 1 component row, got %d", len(*components))
	}
}

func TestBuildDeletePage_複数ページ_1ページ目(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 30)

	content, components := buildDeletePage("guild1", "user1", 0, 30)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	if !strings.Contains(content, "1/2ページ") {
		t.Errorf("expected page info '1/2ページ', got %q", content)
	}
	if !strings.Contains(content, "全30件") {
		t.Errorf("expected total count '全30件', got %q", content)
	}

	// 2 ActionRows (select menu + pagination buttons)
	if len(*components) != 2 {
		t.Errorf("expected 2 component rows, got %d", len(*components))
	}

	// Check select menu has 25 options
	row0 := (*components)[0].(discordgo.ActionsRow)
	selectMenu := row0.Components[0].(discordgo.SelectMenu)
	if len(selectMenu.Options) != 25 {
		t.Errorf("expected 25 options, got %d", len(selectMenu.Options))
	}

	// Check prev button is disabled, next is enabled
	row1 := (*components)[1].(discordgo.ActionsRow)
	prevBtn := row1.Components[0].(discordgo.Button)
	nextBtn := row1.Components[1].(discordgo.Button)
	if !prevBtn.Disabled {
		t.Error("prev button should be disabled on first page")
	}
	if nextBtn.Disabled {
		t.Error("next button should be enabled on first page")
	}
}

func TestBuildDeletePage_複数ページ_最終ページ(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 30)

	content, components := buildDeletePage("guild1", "user1", 1, 30)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	if !strings.Contains(content, "2/2ページ") {
		t.Errorf("expected page info '2/2ページ', got %q", content)
	}

	// Check select menu has 5 options (remaining)
	row0 := (*components)[0].(discordgo.ActionsRow)
	selectMenu := row0.Components[0].(discordgo.SelectMenu)
	if len(selectMenu.Options) != 5 {
		t.Errorf("expected 5 options on last page, got %d", len(selectMenu.Options))
	}

	// Check prev is enabled, next is disabled
	row1 := (*components)[1].(discordgo.ActionsRow)
	prevBtn := row1.Components[0].(discordgo.Button)
	nextBtn := row1.Components[1].(discordgo.Button)
	if prevBtn.Disabled {
		t.Error("prev button should be enabled on last page")
	}
	if !nextBtn.Disabled {
		t.Error("next button should be disabled on last page")
	}
}

func TestBuildDeletePage_ページ番号が範囲外の場合は最終ページ(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 30)

	content, components := buildDeletePage("guild1", "user1", 99, 30)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	if !strings.Contains(content, "2/2ページ") {
		t.Errorf("expected to clamp to last page, got %q", content)
	}
}

func TestBuildDeletePage_ちょうど25件(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 25)

	content, components := buildDeletePage("guild1", "user1", 0, 25)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	// 25 is exactly 1 page, so no pagination buttons
	if strings.Contains(content, "ページ") {
		t.Error("should not contain page info for exactly 25 items")
	}
	if len(*components) != 1 {
		t.Errorf("expected 1 component row, got %d", len(*components))
	}
}

func TestBuildDeletePage_26件は2ページ(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 26)

	content, components := buildDeletePage("guild1", "user1", 0, 26)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	if !strings.Contains(content, "1/2ページ") {
		t.Errorf("expected '1/2ページ', got %q", content)
	}
	if len(*components) != 2 {
		t.Errorf("expected 2 component rows, got %d", len(*components))
	}
}

func TestBuildDeletePage_ボタンCustomIDにページ情報を含む(t *testing.T) {
	setupDeleteTestDB(t)
	seedDeleteSenryus(t, "guild1", "user1", 30)

	_, components := buildDeletePage("guild1", "user1", 0, 30)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	row1 := (*components)[1].(discordgo.ActionsRow)
	nextBtn := row1.Components[1].(discordgo.Button)

	expected := DeletePagePrefix + "guild1:user1:1"
	if nextBtn.CustomID != expected {
		t.Errorf("expected CustomID %q, got %q", expected, nextBtn.CustomID)
	}
}

func TestBuildDeletePage_スポイラー付き川柳のラベル(t *testing.T) {
	setupDeleteTestDB(t)
	spoiler := true
	s := model.Senryu{
		ServerID:  "guild1",
		AuthorID:  "user1",
		Kamigo:    "秘密の句",
		Nakasichi: "中の句だよ",
		Simogo:    "下の句だ",
		Spoiler:   &spoiler,
	}
	if err := db.DB.Create(&s).Error; err != nil {
		t.Fatalf("failed to seed senryu: %v", err)
	}

	_, components := buildDeletePage("guild1", "user1", 0, 1)
	if components == nil {
		t.Fatal("expected non-nil components")
	}

	row0 := (*components)[0].(discordgo.ActionsRow)
	selectMenu := row0.Components[0].(discordgo.SelectMenu)
	label := selectMenu.Options[0].Label
	if !strings.HasPrefix(label, "🔒 ") {
		t.Errorf("expected spoiler prefix, got %q", label)
	}
}

func TestComponentsToSlice_nil(t *testing.T) {
	result := componentsToSlice(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestComponentsToSlice_非nil(t *testing.T) {
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{},
	}
	result := componentsToSlice(&components)
	if len(result) != 1 {
		t.Errorf("expected 1, got %d", len(result))
	}
}
