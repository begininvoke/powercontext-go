package server

import (
	"context"
	"fmt"

	pcruntime "github.com/ob-labs/powercontext-go/internal/runtime"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
)

type applicationStorage struct {
	database *sqlstore.Database
	resource pcruntime.Resource
	dialect  sqlstore.Dialect
}

func openApplicationStorage(ctx context.Context, config DatabaseConfig) (applicationStorage, error) {
	storage := applicationStorage{dialect: sqlstore.SQLiteDialect}
	var err error
	switch config.Kind {
	case "sqlite":
		var dsn string
		dsn, err = SQLiteDSN(config.SQLite.URL)
		if err == nil {
			storage.database, err = sqlstore.OpenSQLite(ctx, sqlstore.SQLiteConfig{
				DSN: dsn, BusyTimeout: config.SQLite.BusyTimeout,
				JournalMode: config.SQLite.JournalMode, ForeignKeys: config.SQLite.ForeignKeys,
				MaxOpenConns: config.SQLite.MaxOpenConns, MaxIdleConns: config.SQLite.MaxIdleConns,
				ConnMaxLifetime: config.SQLite.ConnMaxLifetime,
			})
		}
		storage.resource = storage.database
	case "oceanbase":
		storage.dialect = sqlstore.MySQLDialect
		storage.database, err = sqlstore.OpenOceanBase(ctx, sqlstore.OceanBaseConfig{
			URL:             config.OceanBase.URL,
			MaxOpenConns:    config.OceanBase.MaxOpenConns,
			MaxIdleConns:    config.OceanBase.MaxIdleConns,
			ConnMaxLifetime: config.OceanBase.MaxLifetime,
		})
		storage.resource = storage.database
	case "seekdb":
		storage.dialect = sqlstore.MySQLDialect
		var instance seekDBInstance
		storage.database, instance.value, err = sqlstore.OpenSeekDB(ctx, sqlstore.SeekDBConfig{
			Path: config.SeekDB.Path, Database: config.SeekDB.Database,
			LibraryPath:     config.SeekDB.LibraryPath,
			MaxOpenConns:    config.SeekDB.MaxOpenConns,
			MaxIdleConns:    config.SeekDB.MaxIdleConns,
			ConnMaxLifetime: config.SeekDB.MaxLifetime,
		})
		if err == nil {
			instance.database = storage.database
			storage.resource = &instance
		}
	}
	if err != nil {
		return applicationStorage{}, fmt.Errorf("server: open database: %w", err)
	}
	return storage, nil
}
