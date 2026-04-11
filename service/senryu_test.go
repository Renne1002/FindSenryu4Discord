package service

import (
	"sync"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setupSenryuTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.Senryu{}, &model.MutedChannel{}, &model.GuildChannelTypeSetting{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

// resetCrypto resets the crypto package state for test isolation.
func resetCrypto(t *testing.T) {
	t.Helper()
	// Reset the internal sync.Once and state so we can re-init per test.
	// This accesses unexported fields via the test being in the same module.
	crypto.ResetForTest()
}

func boolPtr(b bool) *bool {
	return &b
}

func TestCreateSenryu_暗号化有効時にDBに平文が保存されない(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Read raw data from DB (no decryption)
	var raw model.Senryu
	if err := db.DB.Where("id = ?", created.ID).First(&raw).Error; err != nil {
		t.Fatalf("failed to read raw senryu: %v", err)
	}

	if raw.Kamigo == "古池や" {
		t.Error("Kamigo should be encrypted in DB, but found plaintext")
	}
	if raw.Nakasichi == "蛙飛び込む" {
		t.Error("Nakasichi should be encrypted in DB, but found plaintext")
	}
	if raw.Simogo == "水の音" {
		t.Error("Simogo should be encrypted in DB, but found plaintext")
	}
}

func TestGetSenryuByID_暗号化有効時に平文が復元される(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "柿くへば",
		Nakasichi: "鐘が鳴るなり",
		Simogo:    "法隆寺",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	got, err := GetSenryuByID(created.ID, "server1")
	if err != nil {
		t.Fatalf("GetSenryuByID failed: %v", err)
	}

	if got.Kamigo != "柿くへば" {
		t.Errorf("Kamigo: expected %q, got %q", "柿くへば", got.Kamigo)
	}
	if got.Nakasichi != "鐘が鳴るなり" {
		t.Errorf("Nakasichi: expected %q, got %q", "鐘が鳴るなり", got.Nakasichi)
	}
	if got.Simogo != "法隆寺" {
		t.Errorf("Simogo: expected %q, got %q", "法隆寺", got.Simogo)
	}
}

func TestGetLastSenryu_暗号化有効時に復号された文字列を返す(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "春すぎて",
		Nakasichi: "夏来にけらし",
		Simogo:    "白妙の",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	result, err := GetLastSenryu("server1", "author1")
	if err != nil {
		t.Fatalf("GetLastSenryu failed: %v", err)
	}

	// Should contain the decrypted content
	if !containsSubstring(result, "春すぎて") {
		t.Errorf("result should contain decrypted Kamigo, got: %s", result)
	}
	if !containsSubstring(result, "夏来にけらし") {
		t.Errorf("result should contain decrypted Nakasichi, got: %s", result)
	}
	if !containsSubstring(result, "白妙の") {
		t.Errorf("result should contain decrypted Simogo, got: %s", result)
	}
}

func TestGetRecentSenryusByAuthor_暗号化有効時に復号されたリストを返す(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "閑さや",
		Nakasichi: "岩にしみ入る",
		Simogo:    "蝉の声",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	senryus, err := GetRecentSenryusByAuthor("server1", "author1", 10)
	if err != nil {
		t.Fatalf("GetRecentSenryusByAuthor failed: %v", err)
	}

	if len(senryus) != 1 {
		t.Fatalf("expected 1 senryu, got %d", len(senryus))
	}

	if senryus[0].Kamigo != "閑さや" {
		t.Errorf("Kamigo: expected %q, got %q", "閑さや", senryus[0].Kamigo)
	}
	if senryus[0].Nakasichi != "岩にしみ入る" {
		t.Errorf("Nakasichi: expected %q, got %q", "岩にしみ入る", senryus[0].Nakasichi)
	}
	if senryus[0].Simogo != "蝉の声" {
		t.Errorf("Simogo: expected %q, got %q", "蝉の声", senryus[0].Simogo)
	}
}

func TestGetThreeRandomSenryus_暗号化有効時に復号されたデータを返す(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	phrases := []struct{ k, n, s string }{
		{"五月雨を", "あつめて早し", "最上川"},
		{"荒海や", "佐渡によこたふ", "天の川"},
		{"夏草や", "兵どもが", "夢の跡"},
	}
	for _, p := range phrases {
		_, err := CreateSenryu(model.Senryu{
			ServerID:  "server1",
			AuthorID:  "author1",
			Kamigo:    p.k,
			Nakasichi: p.n,
			Simogo:    p.s,
			Spoiler:   &spoiler,
		})
		if err != nil {
			t.Fatalf("CreateSenryu failed: %v", err)
		}
	}

	senryus, err := GetThreeRandomSenryus("server1")
	if err != nil {
		t.Fatalf("GetThreeRandomSenryus failed: %v", err)
	}

	if len(senryus) != 3 {
		t.Fatalf("expected 3 senryus, got %d", len(senryus))
	}

	for i, s := range senryus {
		if s.Kamigo == "" || s.Nakasichi == "" || s.Simogo == "" {
			t.Errorf("senryu[%d] has empty fields after decryption", i)
		}
		// Verify it's actual Japanese text, not base64
		if crypto.IsEncrypted(s.Kamigo) {
			t.Errorf("senryu[%d].Kamigo should be decrypted plaintext", i)
		}
	}
}

func TestCreateSenryu_暗号化無効時に平文のまま保存される(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	var raw model.Senryu
	if err := db.DB.Where("id = ?", created.ID).First(&raw).Error; err != nil {
		t.Fatalf("failed to read raw senryu: %v", err)
	}

	if raw.Kamigo != "古池や" {
		t.Errorf("Kamigo should be plaintext when encryption is disabled, got %q", raw.Kamigo)
	}
	if raw.Nakasichi != "蛙飛び込む" {
		t.Errorf("Nakasichi should be plaintext when encryption is disabled, got %q", raw.Nakasichi)
	}
	if raw.Simogo != "水の音" {
		t.Errorf("Simogo should be plaintext when encryption is disabled, got %q", raw.Simogo)
	}
}

func TestMigration_平文データが暗号化される(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)

	// Insert plaintext data with encryption disabled
	if err := crypto.Init(""); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}
	spoiler := false
	_, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "月見れば",
		Nakasichi: "千々にものこそ",
		Simogo:    "悲しけれ",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Enable encryption and run migration
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	// Simulate the migration (same logic as migrateEncryptSenryuData in db.go)
	var senryus []model.Senryu
	db.DB.Find(&senryus)
	for i := range senryus {
		s := &senryus[i]
		if crypto.IsEncrypted(s.Kamigo) {
			continue
		}
		kamigo, _ := crypto.Encrypt(s.Kamigo)
		nakasichi, _ := crypto.Encrypt(s.Nakasichi)
		simogo, _ := crypto.Encrypt(s.Simogo)
		db.DB.Model(s).Updates(map[string]interface{}{
			"kamigo":    kamigo,
			"nakasichi": nakasichi,
			"simogo":    simogo,
		})
	}

	// Verify raw DB data is encrypted
	var raw model.Senryu
	db.DB.First(&raw)
	if raw.Kamigo == "月見れば" {
		t.Error("Kamigo should be encrypted after migration")
	}

	// Verify decryption works correctly via service
	got, err := GetSenryuByID(raw.ID, "server1")
	if err != nil {
		t.Fatalf("GetSenryuByID failed: %v", err)
	}
	if got.Kamigo != "月見れば" {
		t.Errorf("expected %q after decryption, got %q", "月見れば", got.Kamigo)
	}
	if got.Nakasichi != "千々にものこそ" {
		t.Errorf("expected %q after decryption, got %q", "千々にものこそ", got.Nakasichi)
	}
	if got.Simogo != "悲しけれ" {
		t.Errorf("expected %q after decryption, got %q", "悲しけれ", got.Simogo)
	}
}

func TestMigration_暗号化済みデータは再暗号化されない(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "花の色は",
		Nakasichi: "うつりにけりな",
		Simogo:    "いたづらに",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	// Read raw encrypted data
	var before model.Senryu
	db.DB.Where("id = ?", created.ID).First(&before)

	// Run migration again (simulating restart)
	var senryus []model.Senryu
	db.DB.Find(&senryus)
	for i := range senryus {
		s := &senryus[i]
		if crypto.IsEncrypted(s.Kamigo) {
			continue // should skip
		}
		t.Error("should have detected already-encrypted data and skipped")
	}

	// Verify data is unchanged
	var after model.Senryu
	db.DB.Where("id = ?", created.ID).First(&after)
	if before.Kamigo != after.Kamigo {
		t.Error("encrypted data should not be modified by re-migration")
	}
}

func TestDeleteSenryu_暗号化有効時でも削除できる(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	created, err := CreateSenryu(model.Senryu{
		ServerID:  "server1",
		AuthorID:  "author1",
		Kamigo:    "削除テスト",
		Nakasichi: "暗号化されても",
		Simogo:    "消せるはず",
		Spoiler:   &spoiler,
	})
	if err != nil {
		t.Fatalf("CreateSenryu failed: %v", err)
	}

	if err := DeleteSenryu(created.ID, "server1"); err != nil {
		t.Fatalf("DeleteSenryu failed: %v", err)
	}

	_, err = GetSenryuByID(created.ID, "server1")
	if err != ErrSenryuNotFound {
		t.Errorf("expected ErrSenryuNotFound, got %v", err)
	}
}

func TestGetRanking_暗号化有効時でも集計できる(t *testing.T) {
	setupSenryuTestDB(t)
	resetCrypto(t)
	if err := crypto.Init(testEncryptionKey); err != nil {
		t.Fatalf("crypto init failed: %v", err)
	}

	spoiler := false
	for i := 0; i < 3; i++ {
		_, err := CreateSenryu(model.Senryu{
			ServerID:  "server1",
			AuthorID:  "author1",
			Kamigo:    "ランキング",
			Nakasichi: "テストの句です",
			Simogo:    "数えよう",
			Spoiler:   &spoiler,
		})
		if err != nil {
			t.Fatalf("CreateSenryu failed: %v", err)
		}
	}

	ranks, err := GetRanking("server1")
	if err != nil {
		t.Fatalf("GetRanking failed: %v", err)
	}

	if len(ranks) != 1 {
		t.Fatalf("expected 1 rank entry, got %d", len(ranks))
	}
	if ranks[0].Count != 3 {
		t.Errorf("expected count 3, got %d", ranks[0].Count)
	}
	if ranks[0].AuthorId != "author1" {
		t.Errorf("expected author1, got %s", ranks[0].AuthorId)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && contains(s, substr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure we don't import strings just for Contains — keep test deps minimal.
var _ = sync.Once{} // suppress unused import (sync is used by resetCrypto indirectly)
