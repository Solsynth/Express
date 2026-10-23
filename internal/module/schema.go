package module

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// MigrateSchema applies the schema owned by the resolved modules, in
// dependency order. Only enabled modules contribute models or hooks.
func MigrateSchema(ctx context.Context, db *gorm.DB, factories []Factory) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	for _, factory := range factories {
		if len(factory.Models) > 0 {
			if err := db.WithContext(ctx).AutoMigrate(factory.Models...); err != nil {
				return fmt.Errorf("module %s AutoMigrate: %w", factory.ID, err)
			}
		}
		for _, hook := range factory.SQL {
			if hook == nil {
				continue
			}
			if err := hook(ctx, db); err != nil {
				return fmt.Errorf("module %s schema hook: %w", factory.ID, err)
			}
		}
	}
	return nil
}
