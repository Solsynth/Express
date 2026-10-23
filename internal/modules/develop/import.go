// Package developmodule mounts the developer-portal HTTP surfaces. Each module
// owns one product area so a deployment can enable them independently.
package developmodule

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/module"
)

// importer copies the legacy Develop tables into the portal's schema. Every
// query selects columns by name and casts uuid columns to text, so the target's
// string ids line up and the source schema can gain columns without touching
// this file.
//
// The importer is idempotent: rows are inserted with ON CONFLICT (id) DO
// NOTHING, so a re-run reports the rows it skipped instead of rewriting them.
// It never truncates, and it refuses to invent a parent: a child whose parent
// was not imported is skipped and counted, because the target's foreign keys
// would otherwise reject it or orphan it.
type importer struct {
	moduleID string
	// tables are imported in order, so a parent is present before its
	// children are considered.
	tables []tableSpec
}

// tableSpec describes one table copy.
type tableSpec struct {
	// name is the source table and the reported table label.
	name string
	// query selects the source rows. The cast placeholder is replaced with the
	// dialect's uuid-to-text cast so the target's string ids line up; SQLite
	// stores text already and needs none.
	query func(cast string) string
	// scan converts one row into a model, or reports that it must be skipped
	// because a parent is absent.
	scan func(context.Context, *sql.Rows, *importContext) (any, bool, error)
}

// importContext answers "does this parent exist in the target?", so a child
// whose parent is absent is skipped instead of written as a dangling row.
//
// The lookup is a target query rather than a set built during this run: a
// module imports its own tables, so the parent usually landed in an earlier
// module (developers before apps) or in an earlier run. Answers are cached per
// id because a parent is checked once per child row.
type importContext struct {
	target *gorm.DB
	known  map[string]bool
	// planned holds the ids a dry run would have written. Without it a dry run
	// would skip every child, because its parent is not in the target yet.
	planned map[string]bool
	// shared is the plan of every module in this run, so a child module sees
	// its parent's planned rows.
	shared map[string]bool
	dryRun bool
}

func newImportContext(target *gorm.DB, opts module.ImportOptions) *importContext {
	ctx := &importContext{target: target, known: map[string]bool{}, planned: map[string]bool{}, dryRun: opts.DryRun}
	if opts.DryRun && opts.Session != nil {
		ctx.shared = opts.Session.Planned
	}
	return ctx
}

// plan records that a dry run would write this row, so its children validate.
func (c *importContext) plan(table, id string) {
	if !c.dryRun {
		return
	}
	key := table + "\x00" + strings.TrimSpace(id)
	c.planned[key] = true
	if c.shared != nil {
		c.shared[key] = true
	}
}

// exists reports whether a row with this id is present in the target table.
// An empty id, or a lookup that fails, counts as absent: the importer must not
// write a child it cannot tie to a parent.
func (c *importContext) exists(ctx context.Context, table, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	key := table + "\x00" + id
	if c.planned[key] || (c.shared != nil && c.shared[key]) {
		return true
	}
	if cached, ok := c.known[key]; ok {
		return cached
	}
	var count int64
	err := c.target.WithContext(ctx).Table(table).Where("id = ?", id).Count(&count).Error
	found := err == nil && count > 0
	c.known[key] = found
	return found
}

// Import copies every table this module owns.
func (i *importer) Import(ctx context.Context, source, target *gorm.DB, opts module.ImportOptions) (module.ImportStats, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 500
	}
	stats := module.ImportStats{Module: i.moduleID}
	if len(i.tables) == 0 {
		return stats, nil
	}
	stats.Table = i.tables[0].name

	state := newImportContext(target, opts)
	for _, table := range i.tables {
		if !source.Migrator().HasTable(table.name) {
			// A deployment that never used the feature simply has no table.
			// Report it as untouched rather than failing the whole run.
			stats.Tables = append(stats.Tables, module.TableStats{Table: table.name})
			continue
		}
		tableStats, err := importTable(ctx, source, target, opts, table, state)
		if err != nil {
			return stats, fmt.Errorf("%s: %w", table.name, err)
		}
		stats.Tables = append(stats.Tables, tableStats)
		stats.Read += tableStats.Read
		stats.Written += tableStats.Written
		stats.Skipped += tableStats.Skipped
	}

	if opts.DryRun {
		return stats, nil
	}
	for _, table := range stats.Tables {
		if err := module.RecordImport(ctx, target, module.ImportStats{
			Module:  i.moduleID,
			Table:   table.Table,
			Read:    table.Read,
			Written: table.Written,
			Skipped: table.Skipped,
		}); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// importTable copies one table in batches.
func importTable(ctx context.Context, source, target *gorm.DB, opts module.ImportOptions, spec tableSpec, state *importContext) (module.TableStats, error) {
	stats := module.TableStats{Table: spec.name}
	rows, err := source.WithContext(ctx).Raw(spec.query(textCast(source))).Rows()
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	batch := make([]any, 0, opts.BatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if opts.DryRun {
			// The rows were validated and converted; only the write is skipped.
			stats.Written += int64(len(batch))
			batch = batch[:0]
			return nil
		}
		// GORM derives the table from the slice's element type, so a []any
		// would not resolve. Every row in a batch shares one concrete type.
		typed := reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(batch[0])), len(batch), len(batch))
		for index, model := range batch {
			typed.Index(index).Set(reflect.ValueOf(model))
		}
		result := target.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoNothing: true,
		}).CreateInBatches(typed.Interface(), opts.BatchSize)
		if result.Error != nil {
			return result.Error
		}
		stats.Written += result.RowsAffected
		// Rows the target already held count as skipped, not written.
		stats.Skipped += int64(len(batch)) - result.RowsAffected
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		model, skip, err := spec.scan(ctx, rows, state)
		if err != nil {
			return stats, err
		}
		stats.Read++
		if skip {
			stats.Skipped++
			continue
		}
		batch = append(batch, model)
		if len(batch) >= opts.BatchSize {
			if err := flush(); err != nil {
				return stats, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if err := flush(); err != nil {
		return stats, err
	}
	return stats, nil
}

// --- scanning helpers ---

// nullString reads a nullable text column.
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// nullTime reads a nullable timestamp.
func nullTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

// deletedAt converts a nullable source timestamp to the target's soft-delete
// marker, so a row deleted in Develop stays deleted here.
func deletedAt(value sql.NullTime) gorm.DeletedAt {
	if !value.Valid {
		return gorm.DeletedAt{}
	}
	return gorm.DeletedAt{Time: value.Time, Valid: true}
}

// decodeJSON unmarshals a jsonb column into a typed field. A null or empty
// column leaves the target nil.
//
// The legacy source writes these columns with the property names of its own
// model, which are PascalCase, while the portal's struct tags are snake_case.
// A plain unmarshal does not fail on that: it matches no field and leaves the
// value zero, so an oauth_config silently loses every scope and redirect.
// Keys are therefore folded before decoding, always.
func decodeJSON(raw []byte, target any) error {
	if len(raw) == 0 {
		return nil
	}
	normalized, err := normalizeJSONKeys(raw)
	if err != nil {
		return fmt.Errorf("decode jsonb: %w", err)
	}
	if err := json.Unmarshal(normalized, target); err != nil {
		return fmt.Errorf("decode jsonb: %w", err)
	}
	return nil
}

// normalizeJSONKeys rewrites every object key to snake_case, so the source's
// "ClientUri" and the target's "client_uri" match. Array elements and non-string
// scalars are untouched.
func normalizeJSONKeys(raw []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(normalizeJSONValue(value))
}

func normalizeJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized[normalizeJSONKey(key)] = normalizeJSONValue(child)
		}
		return normalized
	case []any:
		for index := range typed {
			typed[index] = normalizeJSONValue(typed[index])
		}
		return typed
	default:
		return value
	}
}

// normalizeJSONKey renders a property name in snake_case. A key that is
// already snake_case is returned unchanged, so the same decoder reads a column
// written here and one written by the legacy source.
func normalizeJSONKey(key string) string {
	key = strings.TrimSpace(key)
	var builder strings.Builder
	builder.Grow(len(key) + 4)
	runes := []rune(key)
	for index, current := range runes {
		if current == '_' {
			builder.WriteRune('_')
			continue
		}
		if isUpperRune(current) {
			previousExists := index > 0
			previous := rune(0)
			if previousExists {
				previous = runes[index-1]
			}
			nextIsLower := index+1 < len(runes) && !isUpperRune(runes[index+1])
			// A boundary is a capital that follows a lower case or digit, or
			// the last capital of an acronym before a lower case letter.
			boundary := previousExists && (isLowerRune(previous) || isDigitRune(previous)) ||
				(previousExists && isUpperRune(previous) && nextIsLower)
			if boundary {
				builder.WriteRune('_')
			}
			builder.WriteRune(unicode.ToLower(current))
			continue
		}
		builder.WriteRune(current)
	}
	return builder.String()
}

func isUpperRune(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLowerRune(r rune) bool { return r >= 'a' && r <= 'z' }
func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }

// cloudFile decodes a jsonb cloud-file reference.
func cloudFile(raw []byte) (*database.CloudFileReference, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var file database.CloudFileReference
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode cloud file: %w", err)
	}
	if strings.TrimSpace(file.Id) == "" && strings.TrimSpace(file.Url) == "" {
		return nil, nil
	}
	return &file, nil
}

// textCast returns the SQL cast that renders a uuid column as text. Postgres
// stores uuids natively; SQLite already stores them as text.
func textCast(db *gorm.DB) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return "::text"
	}
	return ""
}

// idOf reads an id column and reports whether it is usable.
func idOf(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value != ""
}
