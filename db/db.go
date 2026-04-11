package db

import (
	"embed"
	"errors"
	"os"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	sqlitemigrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jinzhu/gorm"
	"github.com/u16-io/FindSenryu4Discord/config"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"

	// SQLite3 driver for Gorm
	_ "github.com/mattn/go-sqlite3"
	// PostgreSQL driver for Gorm
	_ "github.com/lib/pq"
)

var (
	DB   *gorm.DB
	once sync.Once
)

//go:embed migrations/sqlite3/*.sql
var sqliteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

// Init initializes the database connection
func Init() error {
	var initErr error
	once.Do(func() {
		initErr = initDB()
	})
	return initErr
}

func initDB() error {
	conf := config.GetConf()

	// Ensure data directory exists for SQLite
	if conf.Database.Driver == "sqlite3" {
		if _, err := os.Stat("data"); os.IsNotExist(err) {
			if err := os.Mkdir("data", 0755); err != nil {
				logger.Error("Failed to create data directory", "error", err)
				return err
			}
		}
	}

	var err error
	switch conf.Database.Driver {
	case "postgres":
		DB, err = gorm.Open("postgres", conf.Database.DSN)
		if err != nil {
			logger.Error("Failed to connect to PostgreSQL", "error", err)
			return err
		}
		logger.Info("Connected to PostgreSQL database")
	default: // sqlite3
		DB, err = gorm.Open("sqlite3", conf.Database.Path)
		if err != nil {
			logger.Error("Failed to connect to SQLite", "error", err)
			return err
		}

		// Enable WAL mode for better concurrency
		if err := DB.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			logger.Warn("Failed to enable WAL mode", "error", err)
		} else {
			logger.Debug("SQLite WAL mode enabled")
		}

		// Optimize SQLite settings
		DB.Exec("PRAGMA synchronous=NORMAL")
		DB.Exec("PRAGMA cache_size=10000")
		DB.Exec("PRAGMA temp_store=MEMORY")

		logger.Info("Connected to SQLite database", "path", conf.Database.Path)
	}

	// Configure connection pool
	sqlDB := DB.DB()
	if sqlDB != nil {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
	}

	logger.Debug("Database connection established")

	return nil
}

// Migrate runs all schema and data migrations using golang-migrate.
// SQL migration files are embedded per dialect (sqlite3/postgres).
// It must be called after Init().
func Migrate() error {
	if DB == nil {
		return errors.New("database not initialized; call Init() first")
	}

	conf := config.GetConf()
	sqlDB := DB.DB()

	var sourceDriver source.Driver
	var dbDriver database.Driver
	var driverName string
	var err error

	switch conf.Database.Driver {
	case "postgres":
		sourceDriver, err = iofs.New(postgresMigrations, "migrations/postgres")
		if err != nil {
			return err
		}
		dbDriver, err = pgmigrate.WithInstance(sqlDB, &pgmigrate.Config{})
		driverName = "postgres"
	default:
		sourceDriver, err = iofs.New(sqliteMigrations, "migrations/sqlite3")
		if err != nil {
			return err
		}
		dbDriver, err = sqlitemigrate.WithInstance(sqlDB, &sqlitemigrate.Config{})
		driverName = "sqlite3"
	}
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, driverName, dbDriver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	// Guard: refuse to start if DB has encrypted data but encryption is disabled
	if !crypto.IsEnabled() && isEncryptionMigrated() {
		return errors.New("database contains encrypted data but encryption key is not configured; set encryption.key in config")
	}

	// Encrypt existing plaintext senryu data if encryption is enabled
	if crypto.IsEnabled() && !isEncryptionMigrated() {
		if err := migrateEncryptSenryuData(); err != nil {
			logger.Error("Failed to encrypt existing senryu data", "error", err)
			return err
		}
	}

	logger.Info("Database migration completed")

	return nil
}

// isEncryptionMigrated checks the metadata table for encryption migration state.
func isEncryptionMigrated() bool {
	var value string
	row := DB.DB().QueryRow("SELECT value FROM metadata WHERE key = 'encryption_migrated'")
	if err := row.Scan(&value); err != nil {
		return false
	}
	return value == "true"
}

// setEncryptionMigrated marks the encryption migration as completed.
func setEncryptionMigrated() error {
	_, err := DB.DB().Exec(
		"INSERT INTO metadata (key, value) VALUES ('encryption_migrated', 'true') ON CONFLICT (key) DO UPDATE SET value = 'true'",
	)
	return err
}

// migrateEncryptSenryuData encrypts existing plaintext senryu records.
// Uses cursor-based pagination (WHERE id > ?) and wraps each batch in a transaction.
func migrateEncryptSenryuData() error {
	const batchSize = 100
	var total, encrypted int
	var lastID int

	for {
		var senryus []model.Senryu
		if err := DB.Where("id > ?", lastID).Order("id ASC").Limit(batchSize).Find(&senryus).Error; err != nil {
			return err
		}
		if len(senryus) == 0 {
			break
		}

		tx := DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}

		for i := range senryus {
			total++
			s := &senryus[i]
			lastID = s.ID

			if crypto.IsEncrypted(s.Kamigo) {
				continue
			}

			kamigo, err := crypto.Encrypt(s.Kamigo)
			if err != nil {
				tx.Rollback()
				return err
			}
			nakasichi, err := crypto.Encrypt(s.Nakasichi)
			if err != nil {
				tx.Rollback()
				return err
			}
			simogo, err := crypto.Encrypt(s.Simogo)
			if err != nil {
				tx.Rollback()
				return err
			}

			if err := tx.Model(s).Updates(map[string]interface{}{
				"kamigo":    kamigo,
				"nakasichi": nakasichi,
				"simogo":    simogo,
			}).Error; err != nil {
				tx.Rollback()
				return err
			}
			encrypted++
		}

		if err := tx.Commit().Error; err != nil {
			return err
		}
	}

	if encrypted > 0 {
		logger.Info("Encrypted existing senryu data", "encrypted", encrypted, "total", total)
	}

	if err := setEncryptionMigrated(); err != nil {
		return err
	}

	return nil
}

// Close closes the database connection
func Close() error {
	if DB != nil {
		logger.Info("Closing database connection")
		if err := DB.Close(); err != nil {
			logger.Error("Failed to close database connection", "error", err)
			return err
		}
		logger.Info("Database connection closed")
	}
	return nil
}

// IsConnected returns true if database is connected
func IsConnected() bool {
	if DB == nil {
		return false
	}
	sqlDB := DB.DB()
	if sqlDB == nil {
		return false
	}
	return sqlDB.Ping() == nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	return DB
}

// Stats returns database statistics
type Stats struct {
	SenryuCount       int64
	MutedChannelCount int64
	OptOutCount       int64
	IsConnected       bool
}

// GetStats returns database statistics
func GetStats() Stats {
	stats := Stats{
		IsConnected: IsConnected(),
	}

	if DB != nil {
		DB.Model(&model.Senryu{}).Count(&stats.SenryuCount)
		DB.Model(&model.MutedChannel{}).Count(&stats.MutedChannelCount)
		DB.Model(&model.DetectionOptOut{}).Count(&stats.OptOutCount)
	}

	return stats
}
