package service

import (
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"github.com/jinzhu/gorm"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

const metadataKeyContactAdditionalMessage = "contact_additional_message"

// contactAdditionalMessageCache caches the additional message in memory.
// Stored value is *string: nil (zero Value) = not loaded yet,
// empty *string = DB has no row, non-empty *string = cached value.
var contactAdditionalMessageCache atomic.Value

// GetContactAdditionalMessage returns the additional message for the /contact command.
// Returns empty string if not set.
func GetContactAdditionalMessage() (string, error) {
	if cached := contactAdditionalMessageCache.Load(); cached != nil {
		return *cached.(*string), nil
	}

	metrics.RecordDatabaseOperation("get_contact_additional_message")

	var meta model.Metadata
	err := db.DB.Where("key = ?", metadataKeyContactAdditionalMessage).First(&meta).Error
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			empty := ""
			contactAdditionalMessageCache.Store(&empty)
			return "", nil
		}
		metrics.RecordError("database")
		logger.Error("Failed to get contact additional message", "error", err)
		return "", errors.Wrap(err, "failed to get contact additional message")
	}

	contactAdditionalMessageCache.Store(&meta.Value)
	return meta.Value, nil
}

// SetContactAdditionalMessage sets the additional message for the /contact command.
func SetContactAdditionalMessage(message string) error {
	metrics.RecordDatabaseOperation("set_contact_additional_message")

	meta := model.Metadata{Key: metadataKeyContactAdditionalMessage, Value: message}
	if err := db.DB.Where("key = ?", metadataKeyContactAdditionalMessage).
		Assign(model.Metadata{Value: message}).
		FirstOrCreate(&meta).Error; err != nil {
		metrics.RecordError("database")
		logger.Error("Failed to set contact additional message", "error", err)
		return errors.Wrap(err, "failed to set contact additional message")
	}

	msg := message
	contactAdditionalMessageCache.Store(&msg)
	logger.Info("Contact additional message updated", "message_length", len(message))
	return nil
}

// ClearContactAdditionalMessage removes the additional message for the /contact command.
func ClearContactAdditionalMessage() error {
	metrics.RecordDatabaseOperation("clear_contact_additional_message")

	if err := db.DB.Where("key = ?", metadataKeyContactAdditionalMessage).
		Delete(&model.Metadata{}).Error; err != nil {
		metrics.RecordError("database")
		logger.Error("Failed to clear contact additional message", "error", err)
		return errors.Wrap(err, "failed to clear contact additional message")
	}

	empty := ""
	contactAdditionalMessageCache.Store(&empty)
	logger.Info("Contact additional message cleared")
	return nil
}
