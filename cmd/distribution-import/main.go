// distribution-import copies the legacy Develop tables into the developer
// portal's schema, module by module.
//
// It is intentionally separate from the service binary: an import is an
// operator action against two databases, not something a running process
// should do. Every importer is idempotent, never truncates the target, and
// records its counters in module_import_state.
//
// Usage:
//
//	distribution-import \
//	  --module app.solian.develop.apps \
//	  --source-dsn "postgres://.../dyson_develop" \
//	  --target-dsn "postgres://.../dyson_distribution"
//
// Run with --dry-run first: it reads and validates every row, then reports what
// would be written without writing anything.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/database"
	"src.solsynth.dev/solsynth/distribution/internal/module"
	"src.solsynth.dev/solsynth/distribution/internal/modules/standard"
)

func main() {
	var moduleID, sourceDSN, targetDSN string
	var dryRun bool
	var batchSize int
	flag.StringVar(&moduleID, "module", "", `module ID, or "all" for every module with an importer`)
	flag.StringVar(&sourceDSN, "source-dsn", os.Getenv("SOURCE_DSN"), "legacy Develop database DSN")
	flag.StringVar(&targetDSN, "target-dsn", os.Getenv("TARGET_DSN"), "DistributionCenter database DSN (defaults to config)")
	flag.BoolVar(&dryRun, "dry-run", false, "read and validate only; do not write")
	flag.IntVar(&batchSize, "batch-size", 500, "rows per insert batch")
	flag.Parse()

	if moduleID == "" {
		log.Fatal("--module is required")
	}
	if sourceDSN == "" {
		log.Fatal("--source-dsn is required")
	}
	if targetDSN == "" {
		cfg, err := config.Load("")
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		targetDSN = cfg.Database.DSN
	}

	factories := standard.Modules()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	source, err := database.OpenDSN(sourceDSN)
	if err != nil {
		log.Fatalf("connect source: %v", err)
	}
	defer closeDB(source.DB)

	target, err := database.OpenDSN(targetDSN)
	if err != nil {
		log.Fatalf("connect target: %v", err)
	}
	defer closeDB(target.DB)

	opts := module.ImportOptions{DryRun: dryRun, BatchSize: batchSize}
	if dryRun {
		// One session per run, so a child module's validation sees the parent
		// rows an earlier module would have written.
		opts.Session = module.NewImportSession()
	}

	if moduleID == "all" {
		ids := module.ImportOrder(factories)
		if len(ids) == 0 {
			log.Fatal("no modules declare an importer")
		}
		for _, id := range ids {
			factory, ok := findFactory(factories, id)
			if !ok {
				log.Fatalf("unknown module %q", id)
			}
			importer, ok := module.ImporterFor(factories, id)
			if !ok {
				log.Fatalf("module %q has no importer", id)
			}
			if err := runOne(ctx, factory, importer, source.DB, target.DB, opts); err != nil {
				log.Fatalf("%s: %v", id, err)
			}
		}
		return
	}

	factory, ok := findFactory(factories, moduleID)
	if !ok {
		log.Fatalf("unknown module %q", moduleID)
	}
	importer, ok := module.ImporterFor(factories, moduleID)
	if !ok {
		log.Fatalf("module %q has no importer", moduleID)
	}
	if err := runOne(ctx, factory, importer, source.DB, target.DB, opts); err != nil {
		log.Fatalf("%s: %v", moduleID, err)
	}
}

// runOne prepares the target schema, then imports one module.
func runOne(ctx context.Context, factory module.Factory, importer module.Importer, source, target *gorm.DB, opts module.ImportOptions) error {
	if len(factory.Models) > 0 {
		if err := module.MigrateSchema(ctx, target, []module.Factory{factory}); err != nil {
			return fmt.Errorf("prepare target schema: %w", err)
		}
	}
	stats, err := importer.Import(ctx, source, target, opts)
	if err != nil {
		return err
	}
	mode := "imported"
	if opts.DryRun {
		mode = "dry-run"
	}
	// Report each table: a child that skipped rows because its parent was
	// absent must be visible where it happened.
	for _, table := range stats.Tables {
		log.Printf("%s %s %s: read=%d written=%d skipped=%d",
			mode, stats.Module, table.Table, table.Read, table.Written, table.Skipped)
	}
	return nil
}

func findFactory(factories []module.Factory, id string) (module.Factory, bool) {
	for _, factory := range factories {
		if factory.ID == id {
			return factory, true
		}
	}
	return module.Factory{}, false
}

func closeDB(database *gorm.DB) {
	if database == nil {
		return
	}
	sqlDB, err := database.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
