package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestInit_有効なキーで初期化できる(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if !IsEnabled() {
		t.Error("expected encryption to be enabled")
	}
}

func TestInit_空キーで暗号化無効(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if IsEnabled() {
		t.Error("expected encryption to be disabled with empty key")
	}
}

func TestInit_不正なキー長でエラー(t *testing.T) {
	// 16 bytes = 32 hex chars (too short for AES-256)
	err := Init("0123456789abcdef0123456789abcdef")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestInit_不正なhexでエラー(t *testing.T) {
	err := Init("not-a-valid-hex-string-at-all!!")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestInit_エラー後に正しいキーで再初期化できる(t *testing.T) {
	// First call with bad key
	if err := Init("badkey"); err == nil {
		t.Fatal("expected error for bad key")
	}
	if IsEnabled() {
		t.Error("should be disabled after error")
	}

	// Second call with valid key should succeed
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("re-Init with valid key failed: %v", err)
	}
	if !IsEnabled() {
		t.Error("should be enabled after successful re-Init")
	}
}

func TestEncryptDecrypt_ラウンドトリップ(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	plaintext := "古池や"
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if encrypted == plaintext {
		t.Error("encrypted text should differ from plaintext")
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestEncryptDecrypt_空文字列(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	encrypted, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != "" {
		t.Errorf("expected empty string, got %q", decrypted)
	}
}

func TestEncrypt_暗号化無効時は平文を返す(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	plaintext := "蛙飛び込む"
	result, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if result != plaintext {
		t.Errorf("expected plaintext passthrough, got %q", result)
	}
}

func TestDecrypt_暗号化無効時は入力をそのまま返す(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	input := "水の音"
	result, err := Decrypt(input)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if result != input {
		t.Errorf("expected passthrough, got %q", result)
	}
}

func TestDecrypt_不正なbase64でエラー(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := Decrypt("これは不正なbase64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecrypt_短すぎるデータでエラー(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Valid base64 but too short to contain nonce
	short := base64.StdEncoding.EncodeToString([]byte("abc"))
	_, err := Decrypt(short)
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

func TestDecrypt_異なるキーで復号失敗(t *testing.T) {
	key1 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key1); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	encrypted, err := Encrypt("秘密のデータ")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Re-init with a different key
	key2 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := Init(key2); err != nil {
		t.Fatalf("Init failed with key2: %v", err)
	}

	_, err = Decrypt(encrypted)
	if err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestEncrypt_同じ平文でも異なる暗号文を生成(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	plaintext := "柿くへば鐘が鳴るなり法隆寺"
	enc1, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1 failed: %v", err)
	}
	enc2, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}

	if enc1 == enc2 {
		t.Error("encrypting the same plaintext twice should produce different ciphertexts (random nonce)")
	}

	// Both should decrypt to the same plaintext
	dec1, _ := Decrypt(enc1)
	dec2, _ := Decrypt(enc2)
	if dec1 != plaintext || dec2 != plaintext {
		t.Error("both ciphertexts should decrypt to the original plaintext")
	}
}

func TestIsEncrypted_暗号化済みデータを検出(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	encrypted, err := Encrypt("テストデータ")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if !IsEncrypted(encrypted) {
		t.Error("encrypted data should be detected as encrypted")
	}
}

func TestIsEncrypted_平文は暗号化済みと判定しない(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := Init(key); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	plaintexts := []string{
		"古池や",
		"蛙飛び込む",
		"水の音",
		"",
		"これは普通の日本語テキストです",
	}

	for _, pt := range plaintexts {
		if IsEncrypted(pt) {
			t.Errorf("plaintext %q should not be detected as encrypted", pt)
		}
	}
}

func TestIsEncrypted_異なるキーの暗号文は検出しない(t *testing.T) {
	// Encrypt with key1 (manually, outside package state)
	key1Bytes, _ := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	block1, _ := aes.NewCipher(key1Bytes)
	gcm1, _ := cipher.NewGCM(block1)
	nonce := make([]byte, gcm1.NonceSize())
	ciphertext := gcm1.Seal(nonce, nonce, []byte("test"), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)

	// Init with key2
	key2 := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := Init(key2); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if IsEncrypted(encoded) {
		t.Error("data encrypted with a different key should not be detected as encrypted")
	}
}
