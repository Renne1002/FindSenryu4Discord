package service

import (
	"math/rand"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/jinzhu/gorm"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

var (
	ErrSenryuNotFound  = errors.New("senryu not found")
	ErrDatabaseError   = errors.New("database error")
	ErrEncryptFailed   = errors.New("failed to encrypt senryu fields")
	ErrDecryptFailed   = errors.New("failed to decrypt senryu fields")
)

// encryptSenryuFields encrypts the content fields (Kamigo, Nakasichi, Simogo) in place.
func encryptSenryuFields(s *model.Senryu) error {
	if !crypto.IsEnabled() {
		return nil
	}
	var err error
	if s.Kamigo, err = crypto.Encrypt(s.Kamigo); err != nil {
		return errors.Wrap(ErrEncryptFailed, err.Error())
	}
	if s.Nakasichi, err = crypto.Encrypt(s.Nakasichi); err != nil {
		return errors.Wrap(ErrEncryptFailed, err.Error())
	}
	if s.Simogo, err = crypto.Encrypt(s.Simogo); err != nil {
		return errors.Wrap(ErrEncryptFailed, err.Error())
	}
	return nil
}

// decryptSenryuFields decrypts the content fields (Kamigo, Nakasichi, Simogo) in place.
func decryptSenryuFields(s *model.Senryu) error {
	if !crypto.IsEnabled() {
		return nil
	}
	var err error
	if s.Kamigo, err = crypto.Decrypt(s.Kamigo); err != nil {
		return errors.Wrap(ErrDecryptFailed, err.Error())
	}
	if s.Nakasichi, err = crypto.Decrypt(s.Nakasichi); err != nil {
		return errors.Wrap(ErrDecryptFailed, err.Error())
	}
	if s.Simogo, err = crypto.Decrypt(s.Simogo); err != nil {
		return errors.Wrap(ErrDecryptFailed, err.Error())
	}
	return nil
}

// decryptSenryuSlice decrypts content fields for all senryus in the slice.
func decryptSenryuSlice(senryus []model.Senryu) error {
	for i := range senryus {
		if err := decryptSenryuFields(&senryus[i]); err != nil {
			return err
		}
	}
	return nil
}

// CreateSenryu creates a new senryu record
func CreateSenryu(s model.Senryu) (model.Senryu, error) {
	metrics.RecordDatabaseOperation("create_senryu")

	// Encrypt a copy for DB storage; keep the original fields intact for the caller
	dbRecord := s
	if err := encryptSenryuFields(&dbRecord); err != nil {
		logger.Error("Failed to encrypt senryu", "error", err)
		return s, err
	}

	if err := db.DB.Create(&dbRecord).Error; err != nil {
		metrics.RecordError("database")
		logger.Error("Failed to create senryu",
			"error", err,
			"server_id", s.ServerID,
			"author_id", s.AuthorID,
		)
		return s, errors.Wrap(err, "failed to create senryu")
	}

	// Copy DB-assigned fields back to the plaintext version
	s.ID = dbRecord.ID
	s.CreatedAt = dbRecord.CreatedAt

	metrics.RecordSenryuDetected(s.ServerID)
	logger.Debug("Senryu created",
		"id", s.ID,
		"server_id", s.ServerID,
		"author_id", s.AuthorID,
	)
	return s, nil
}

// GetLastSenryu returns the last senryu in a server
func GetLastSenryu(serverID string, userID string) (string, error) {
	metrics.RecordDatabaseOperation("get_last_senryu")

	s := model.Senryu{}
	if err := db.DB.Where(&model.Senryu{ServerID: serverID}).Last(&s).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return "", ErrSenryuNotFound
		}
		metrics.RecordError("database")
		logger.Warn("Failed to get last senryu",
			"error", err,
			"server_id", serverID,
		)
		return "", errors.Wrap(err, "failed to get last senryu")
	}

	if err := decryptSenryuFields(&s); err != nil {
		logger.Error("Failed to decrypt senryu", "error", err, "id", s.ID)
		return "", err
	}

	var str string
	if userID == s.AuthorID {
		str = "お前"
	} else {
		str = "<@" + s.AuthorID + "> "
	}
	if s.Spoiler != nil && *s.Spoiler {
		str += "が||「" + s.Kamigo + " " + s.Nakasichi + " " + s.Simogo + "」||って詠んだのが最後やぞ"
	} else {
		str += "が「" + s.Kamigo + " " + s.Nakasichi + " " + s.Simogo + "」って詠んだのが最後やぞ"
	}

	return str, nil
}

// GetThreeRandomSenryus returns three random senryus for generating a new one
func GetThreeRandomSenryus(serverID string) ([]model.Senryu, error) {
	metrics.RecordDatabaseOperation("get_random_senryus")

	var count int64
	if err := db.DB.Model(&model.Senryu{}).Where("server_id = ? AND spoiler = ?", serverID, false).Count(&count).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to count senryus",
			"error", err,
			"server_id", serverID,
		)
		return nil, errors.Wrap(err, "failed to count senryus")
	}

	if count == 0 {
		return nil, nil
	}

	result := make([]model.Senryu, 0, 3)
	for i := 0; i < 3; i++ {
		var s model.Senryu
		offset := rand.Intn(int(count))
		if err := db.DB.Where("server_id = ? AND spoiler = ?", serverID, false).Offset(offset).Limit(1).First(&s).Error; err != nil {
			metrics.RecordError("database")
			logger.Warn("Failed to get random senryu",
				"error", err,
				"server_id", serverID,
			)
			return nil, errors.Wrap(err, "failed to get random senryu")
		}
		result = append(result, s)
	}

	if err := decryptSenryuSlice(result); err != nil {
		logger.Error("Failed to decrypt random senryus", "error", err)
		return nil, err
	}

	return result, nil
}

// RankResult represents a ranking entry
type RankResult struct {
	Count    int
	AuthorId string
	Rank     int
}

// GetRanking returns the senryu ranking for a server
func GetRanking(serverID string) ([]RankResult, error) {
	metrics.RecordDatabaseOperation("get_ranking")

	var ranks []RankResult
	if err := db.DB.Model(&model.Senryu{}).
		Where(&model.Senryu{ServerID: serverID}).
		Group("author_id").
		Select("COUNT(TRUE) AS count, author_id").
		Order("count DESC").
		Scan(&ranks).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get ranking",
			"error", err,
			"server_id", serverID,
		)
		return nil, errors.Wrap(err, "failed to get ranking")
	}

	var results []RankResult
	var before RankResult
	for i, rank := range ranks {
		if rank.Count == before.Count {
			rank.Rank = before.Rank
		} else {
			rank.Rank = i + 1
		}
		if rank.Rank > 5 {
			break
		}
		results = append(results, rank)
		before = rank
	}

	return results, nil
}

// GetRecentSenryusByAuthor returns recent senryus by a specific author in a server
func GetRecentSenryusByAuthor(serverID, authorID string, limit int) ([]model.Senryu, error) {
	metrics.RecordDatabaseOperation("get_recent_senryus_by_author")

	var senryus []model.Senryu
	if err := db.DB.Where("server_id = ? AND author_id = ?", serverID, authorID).
		Order("id DESC").Limit(limit).Find(&senryus).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get recent senryus by author",
			"error", err,
			"server_id", serverID,
			"author_id", authorID,
		)
		return nil, errors.Wrap(err, "failed to get recent senryus by author")
	}

	if err := decryptSenryuSlice(senryus); err != nil {
		logger.Error("Failed to decrypt senryus by author", "error", err)
		return nil, err
	}

	return senryus, nil
}

// GetSenryuByID returns a senryu by ID within a server
func GetSenryuByID(id int, serverID string) (*model.Senryu, error) {
	metrics.RecordDatabaseOperation("get_senryu_by_id")

	var s model.Senryu
	if err := db.DB.Where("id = ? AND server_id = ?", id, serverID).First(&s).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return nil, ErrSenryuNotFound
		}
		metrics.RecordError("database")
		logger.Warn("Failed to get senryu by ID",
			"error", err,
			"id", id,
			"server_id", serverID,
		)
		return nil, errors.Wrap(err, "failed to get senryu by ID")
	}

	if err := decryptSenryuFields(&s); err != nil {
		logger.Error("Failed to decrypt senryu", "error", err, "id", s.ID)
		return nil, err
	}

	return &s, nil
}

// DeleteSenryu deletes a senryu by ID within a server
func DeleteSenryu(id int, serverID string) error {
	metrics.RecordDatabaseOperation("delete_senryu")

	result := db.DB.Where("id = ? AND server_id = ?", id, serverID).Delete(&model.Senryu{})
	if result.Error != nil {
		metrics.RecordError("database")
		logger.Error("Failed to delete senryu",
			"error", result.Error,
			"id", id,
			"server_id", serverID,
		)
		return errors.Wrap(result.Error, "failed to delete senryu")
	}

	if result.RowsAffected == 0 {
		return ErrSenryuNotFound
	}

	logger.Info("Senryu deleted",
		"id", id,
		"server_id", serverID,
	)
	return nil
}

// DeleteSenryuByServer deletes all senryus belonging to a server
func DeleteSenryuByServer(serverID string) (int64, error) {
	metrics.RecordDatabaseOperation("delete_senryu_by_server")

	result := db.DB.Where("server_id = ?", serverID).Delete(&model.Senryu{})
	if result.Error != nil {
		metrics.RecordError("database")
		logger.Error("Failed to delete senryus by server",
			"error", result.Error,
			"server_id", serverID,
		)
		return 0, errors.Wrap(result.Error, "failed to delete senryus by server")
	}

	logger.Info("Senryus deleted by server",
		"server_id", serverID,
		"count", result.RowsAffected,
	)
	return result.RowsAffected, nil
}

// CountUniqueAuthorsByDateRange returns the number of unique authors who created senryus within [from, to)
func CountUniqueAuthorsByDateRange(from, to time.Time) (int64, error) {
	metrics.RecordDatabaseOperation("count_unique_authors_by_date_range")

	var count int64
	if err := db.DB.Model(&model.Senryu{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Select("COUNT(DISTINCT author_id)").
		Count(&count).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to count unique authors by date range",
			"error", err,
			"from", from,
			"to", to,
		)
		return 0, errors.Wrap(err, "failed to count unique authors by date range")
	}

	return count, nil
}

// CountSenryuByDateRange returns the count of senryus created within the given time range [from, to)
func CountSenryuByDateRange(from, to time.Time) (int64, error) {
	metrics.RecordDatabaseOperation("count_senryu_by_date_range")

	var count int64
	if err := db.DB.Model(&model.Senryu{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Count(&count).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to count senryus by date range",
			"error", err,
			"from", from,
			"to", to,
		)
		return 0, errors.Wrap(err, "failed to count senryus by date range")
	}

	return count, nil
}

// GetServerStats returns statistics for a server
type ServerStats struct {
	TotalSenryus  int64
	UniqueAuthors int64
}

// GetServerStats returns statistics for a server
func GetServerStats(serverID string) (ServerStats, error) {
	metrics.RecordDatabaseOperation("get_server_stats")

	var stats ServerStats

	if err := db.DB.Model(&model.Senryu{}).Where(&model.Senryu{ServerID: serverID}).Count(&stats.TotalSenryus).Error; err != nil {
		return stats, errors.Wrap(err, "failed to count senryus")
	}

	var count int64
	if err := db.DB.Model(&model.Senryu{}).Where(&model.Senryu{ServerID: serverID}).Select("COUNT(DISTINCT author_id)").Count(&count).Error; err != nil {
		return stats, errors.Wrap(err, "failed to count unique authors")
	}
	stats.UniqueAuthors = count

	return stats, nil
}
