package database

import (
	"errors"
	"fmt"

	"watgbridge/state"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func hasKeys(p_map *map[string]string, keys ...string) (missingKeys []string) {
	m := *p_map
	for _, key := range keys {
		_, exists := m[key]
		if !exists {
			missingKeys = append(missingKeys, key)
		}
	}
	return missingKeys
}

func Connect() (*gorm.DB, error) {
	dbConfig := state.State.Config.Database
	dbType, exists := dbConfig["type"]
	if !exists {
		return nil, errors.New("key 'type' not found in database config : set database.type to 'sqlite', 'postgres' or 'mysql' (see sample_config.yaml)")
	}

	gormConfig := gorm.Config{}

	switch dbType {

	case "postgres":

		if missingKeys := hasKeys(&state.State.Config.Database,
			"host", "user", "password", "dbname", "port", "time_zone",
		); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		var dns string
		dns += "host=" + dbConfig["host"]
		dns += " user=" + dbConfig["user"]
		dns += " password=" + dbConfig["password"]
		dns += " dbname=" + dbConfig["dbname"]
		dns += " port=" + dbConfig["port"]
		dns += " TimeZone=" + dbConfig["time_zone"]
		if _, found := dbConfig["ssl"]; !found {
			dns += " sslmode=disable"
		}

		return gorm.Open(postgres.Open(dns), &gormConfig)

	case "sqlite":

		if missingKeys := hasKeys(&state.State.Config.Database, "path"); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		return openSQLiteDatabase(dbConfig["path"], &gormConfig)

	case "mysql":

		if missingKeys := hasKeys(&state.State.Config.Database,
			"user", "password", "host", "port", "dbname",
		); len(missingKeys) != 0 {
			return nil, fmt.Errorf("Error: database config for type '%s' requires the keys %+v", dbType, missingKeys)
		}

		dns := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			dbConfig["user"],
			dbConfig["password"],
			dbConfig["host"],
			dbConfig["port"],
			dbConfig["dbname"],
		)

		return gorm.Open(mysql.Open(dns), &gormConfig)
	}

	return nil, fmt.Errorf("Database of type '%s' is not supported", dbType)
}

// openSQLiteDatabase configures a single shared SQLite writer for the bridge.
// Multiple GORM pooled SQLite connections can otherwise race and return
// "database is locked" when contacts, routes and receipts arrive together.
func openSQLiteDatabase(path string, cfg *gorm.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), cfg)
	if err != nil { return nil, err }
	pool, err := db.DB()
	if err != nil { return nil, err }
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err := db.Exec("PRAGMA busy_timeout = 10000").Error; err != nil {
		return nil, fmt.Errorf("SQLite busy timeout setup failed: %w", err)
	}
	if err := db.Exec("PRAGMA journal_mode = WAL").Error; err != nil {
		return nil, fmt.Errorf("SQLite WAL setup failed: %w", err)
	}
	return db, nil
}
