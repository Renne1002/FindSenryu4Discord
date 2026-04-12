package service

import (
	"sync/atomic"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
)

func setupContactTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.Metadata{})
	// Reset cache to zero value (nil = not loaded) for test isolation
	contactAdditionalMessageCache = atomic.Value{}
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestGetContactAdditionalMessage_未設定時は空文字を返す(t *testing.T) {
	setupContactTestDB(t)

	msg, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" {
		t.Errorf("expected empty string, got %q", msg)
	}
}

func TestSetContactAdditionalMessage_設定と取得(t *testing.T) {
	setupContactTestDB(t)

	want := "テストメッセージです"
	if err := SetContactAdditionalMessage(want); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestSetContactAdditionalMessage_更新の冪等性(t *testing.T) {
	setupContactTestDB(t)

	// 初回設定
	if err := SetContactAdditionalMessage("初回メッセージ"); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	// 上書き更新
	want := "更新メッセージ"
	if err := SetContactAdditionalMessage(want); err != nil {
		t.Fatalf("failed to update message: %v", err)
	}

	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	// DBに1行しかないことを確認
	var count int
	if err := db.DB.Model(&model.Metadata{}).Where("key = ?", metadataKeyContactAdditionalMessage).Count(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestClearContactAdditionalMessage_クリア後に空文字を返す(t *testing.T) {
	setupContactTestDB(t)

	if err := SetContactAdditionalMessage("削除されるメッセージ"); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	if err := ClearContactAdditionalMessage(); err != nil {
		t.Fatalf("failed to clear message: %v", err)
	}

	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string after clear, got %q", got)
	}
}

func TestClearContactAdditionalMessage_未設定時のクリアはエラーにならない(t *testing.T) {
	setupContactTestDB(t)

	if err := ClearContactAdditionalMessage(); err != nil {
		t.Fatalf("unexpected error clearing non-existent message: %v", err)
	}
}

func TestGetContactAdditionalMessage_キャッシュが効く(t *testing.T) {
	setupContactTestDB(t)

	want := "キャッシュテスト"
	if err := SetContactAdditionalMessage(want); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	// DBから直接削除してキャッシュのみにする
	db.DB.Where("key = ?", metadataKeyContactAdditionalMessage).Delete(&model.Metadata{})

	// キャッシュから返される
	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != want {
		t.Errorf("expected cached value %q, got %q", want, got)
	}
}

func TestSetContactAdditionalMessage_同じ値の再設定(t *testing.T) {
	setupContactTestDB(t)

	msg := "同じメッセージ"
	for i := 0; i < 3; i++ {
		if err := SetContactAdditionalMessage(msg); err != nil {
			t.Fatalf("iteration %d: failed to set message: %v", i, err)
		}
	}

	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != msg {
		t.Errorf("expected %q, got %q", msg, got)
	}
}

func TestSetContactAdditionalMessage_空文字の設定(t *testing.T) {
	setupContactTestDB(t)

	// まず値を設定
	if err := SetContactAdditionalMessage("何か"); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	// 空文字で上書き
	if err := SetContactAdditionalMessage(""); err != nil {
		t.Fatalf("failed to set empty message: %v", err)
	}

	got, err := GetContactAdditionalMessage()
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestClearContactAdditionalMessage_DBから行が削除される(t *testing.T) {
	setupContactTestDB(t)

	if err := SetContactAdditionalMessage("削除確認用"); err != nil {
		t.Fatalf("failed to set message: %v", err)
	}

	if err := ClearContactAdditionalMessage(); err != nil {
		t.Fatalf("failed to clear message: %v", err)
	}

	var count int
	if err := db.DB.Model(&model.Metadata{}).Where("key = ?", metadataKeyContactAdditionalMessage).Count(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after clear, got %d", count)
	}
}
