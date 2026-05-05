package service

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
)

var (
	ErrWordInvalid       = errors.New("word is invalid")
	ErrWordAlreadyBanned = errors.New("word already banned")
	ErrWordNotFound      = errors.New("word not found")
)

type wordBanStore struct {
	mu    sync.RWMutex
	path  string
	words map[string]struct{}
}

var globalWordBan = &wordBanStore{words: map[string]struct{}{}}

// InitWordBan loads banned words from a text file.
// The file is created when it does not exist.
func InitWordBan(path string) error {
	globalWordBan.mu.Lock()
	defer globalWordBan.mu.Unlock()

	if err := ensureWordBanFile(path); err != nil {
		return err
	}

	words, err := readWordBanFile(path)
	if err != nil {
		return err
	}

	globalWordBan.path = path
	globalWordBan.words = words
	logger.Info("Word ban list loaded", "path", path, "count", len(words))
	return nil
}

// ListBannedWords returns all banned words sorted asc.
func ListBannedWords() []string {
	globalWordBan.mu.RLock()
	defer globalWordBan.mu.RUnlock()
	return sortedWords(globalWordBan.words)
}

// AddBannedWord adds a word, persists it to file, and deletes matching senryus.
func AddBannedWord(raw string) (string, []model.Senryu, error) {
	word, err := normalizeWord(raw)
	if err != nil {
		return "", nil, err
	}

	globalWordBan.mu.Lock()
	if _, ok := globalWordBan.words[word]; ok {
		globalWordBan.mu.Unlock()
		return "", nil, ErrWordAlreadyBanned
	}
	if err := appendWordBanFile(globalWordBan.path, word); err != nil {
		globalWordBan.mu.Unlock()
		return "", nil, err
	}
	globalWordBan.words[word] = struct{}{}
	globalWordBan.mu.Unlock()

	deleted, err := deleteSenryusContainingAny([]string{word})
	if err != nil {
		return "", nil, err
	}

	return word, deleted, nil
}

// DeleteBannedWord removes a word and rewrites file contents.
func DeleteBannedWord(raw string) (string, error) {
	word, err := normalizeWord(raw)
	if err != nil {
		return "", err
	}

	globalWordBan.mu.Lock()
	defer globalWordBan.mu.Unlock()

	if _, ok := globalWordBan.words[word]; !ok {
		return "", ErrWordNotFound
	}
	delete(globalWordBan.words, word)

	if err := rewriteWordBanFile(globalWordBan.path, globalWordBan.words); err != nil {
		return "", err
	}

	return word, nil
}

// MatchBannedWords checks if text contains banned words.
func MatchBannedWords(text string) (bool, []string) {
	globalWordBan.mu.RLock()
	defer globalWordBan.mu.RUnlock()

	if len(globalWordBan.words) == 0 {
		return false, nil
	}

	matches := make([]string, 0)
	for w := range globalWordBan.words {
		if strings.Contains(text, w) {
			matches = append(matches, w)
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	sort.Strings(matches)
	return true, matches
}

func normalizeWord(raw string) (string, error) {
	word := strings.TrimSpace(raw)
	if word == "" {
		return "", ErrWordInvalid
	}
	for _, r := range word {
		if unicode.IsSpace(r) {
			return "", ErrWordInvalid
		}
	}
	return word, nil
}

func ensureWordBanFile(path string) error {
	if path == "" {
		return errors.Wrap(ErrWordInvalid, "path is empty")
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

func readWordBanFile(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	words := map[string]struct{}{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		w, err := normalizeWord(line)
		if err != nil {
			logger.Warn("Skipping invalid banned word line", "line", line)
			continue
		}
		words[w] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return words, nil
}

func appendWordBanFile(path, word string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(word + "\n"); err != nil {
		return err
	}
	return nil
}

func rewriteWordBanFile(path string, words map[string]struct{}) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, w := range sortedWords(words) {
		if _, err := f.WriteString(w + "\n"); err != nil {
			return err
		}
	}
	return nil
}

func sortedWords(words map[string]struct{}) []string {
	out := make([]string, 0, len(words))
	for w := range words {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

func deleteSenryusContainingAny(words []string) ([]model.Senryu, error) {
	if len(words) == 0 {
		return nil, nil
	}

	var all []model.Senryu
	if err := db.DB.Order("id ASC").Find(&all).Error; err != nil {
		return nil, errors.Wrap(err, "failed to list senryus for wordban")
	}
	if len(all) == 0 {
		return nil, nil
	}

	ids := make([]int, 0)
	deleted := make([]model.Senryu, 0)
	for i := range all {
		s := all[i]
		if err := decryptSenryuFields(&s); err != nil {
			logger.Warn("Failed to decrypt senryu while scanning wordban", "id", s.ID, "error", err)
			continue
		}
		text := fmt.Sprintf("%s %s %s", s.Kamigo, s.Nakasichi, s.Simogo)
		for _, w := range words {
			if strings.Contains(text, w) {
				ids = append(ids, s.ID)
				deleted = append(deleted, s)
				break
			}
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	if err := db.DB.Where("id in (?)", ids).Delete(&model.Senryu{}).Error; err != nil {
		return nil, errors.Wrap(err, "failed to delete senryus by wordban")
	}

	return deleted, nil
}
