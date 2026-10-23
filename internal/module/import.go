package module

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ImportOptions controls a module data import.
type ImportOptions struct {
	DryRun    bool
	BatchSize int
	// Session is shared by every module in one run. A dry run needs it so a
	// child module can see the parent rows an earlier module would have
	// written; a real run leaves it nil because the target holds them.
	Session *ImportSession
}

// ImportSession is the cross-module state of one import run.
type ImportSession struct {
	// Planned holds the "<table>\x00<id>" keys a dry run would have written.
	Planned map[string]bool
}

// NewImportSession creates the shared state for a dry run.
func NewImportSession() *ImportSession {
	return &ImportSession{Planned: map[string]bool{}}
}

// ImportStats reports one module import run. Table names the primary table;
// Read, Written and Skipped cover every table the run touched. Tables breaks
// the totals down when a module imports more than one.
type ImportStats struct {
	Module  string
	Table   string
	Read    int64
	Written int64
	Skipped int64
	Tables  []TableStats
}

// TableStats reports one table within an import run.
type TableStats struct {
	Table   string
	Read    int64
	Written int64
	Skipped int64
}

// Add folds another run's counters into the receiver, leaving the per-table
// breakdown in place.
func (s ImportStats) Add(other ImportStats) ImportStats {
	s.Read += other.Read
	s.Written += other.Written
	s.Skipped += other.Skipped
	s.Tables = append(s.Tables, other.Tables...)
	return s
}

// Importer copies legacy rows into a module's schema. Implementations must be
// idempotent, must not truncate the target, and must honor DryRun.
type Importer interface {
	Import(ctx context.Context, source, target *gorm.DB, opts ImportOptions) (ImportStats, error)
}

// ImporterFor finds the importer for a module ID.
func ImporterFor(factories []Factory, id string) (Importer, bool) {
	for _, factory := range factories {
		if factory.ID != id || factory.Importer == nil {
			continue
		}
		importer := factory.Importer()
		if importer == nil {
			return nil, false
		}
		return importer, true
	}
	return nil, false
}

// importStateTable records which modules have been imported and how much they
// moved. It is created on demand: a deployment that never imports has no such
// table.
const importStateTable = "module_import_state"

// RecordImport persists one module's import counters, creating the state table
// when it is missing. Re-running an import overwrites the counters rather than
// appending, so the table always describes the latest run.
func RecordImport(ctx context.Context, target *gorm.DB, stats ImportStats) error {
	// The timestamp column type and its default differ per dialect: Postgres
	// has timestamptz and now(), SQLite has neither.
	timestampType, timestampDefault := "timestamptz", "now()"
	if target.Dialector != nil && target.Dialector.Name() == "sqlite" {
		timestampType, timestampDefault = "datetime", "CURRENT_TIMESTAMP"
	}
	if err := target.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS ` + importStateTable + ` (
			module text NOT NULL,
			table_name text NOT NULL,
			rows_read bigint NOT NULL,
			rows_written bigint NOT NULL,
			completed_at ` + timestampType + ` NOT NULL DEFAULT ` + timestampDefault + `,
			PRIMARY KEY (module, table_name)
		)`).Error; err != nil {
		return err
	}
	return target.WithContext(ctx).Exec(`
		INSERT INTO `+importStateTable+` (module, table_name, rows_read, rows_written)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (module, table_name) DO UPDATE SET
			rows_read = EXCLUDED.rows_read,
			rows_written = EXCLUDED.rows_written,
			completed_at = `+timestampDefault,
		stats.Module, stats.Table, stats.Read, stats.Written).Error
}

// ImportOrder returns the module IDs that declare an importer, in dependency
// order, so a caller can import a whole deployment in one pass.
func ImportOrder(factories []Factory) []string {
	ids := make([]string, 0, len(factories))
	for _, factory := range factories {
		if factory.Importer != nil {
			ids = append(ids, factory.ID)
		}
	}
	return ids
}

// PrepareImportSchema creates the tables an importer writes into. It runs the
// same AutoMigrate a module applies at startup, so an import can target a
// database that has never run the service.
func PrepareImportSchema(ctx context.Context, target *gorm.DB, factories []Factory) error {
	if target == nil {
		return fmt.Errorf("database is required")
	}
	return MigrateSchema(ctx, target, factories)
}
