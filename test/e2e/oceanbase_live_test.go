package e2e_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/ob-labs/powercontext-go/handoffreport"
	"github.com/ob-labs/powercontext-go/internal/sqlstore"
	"github.com/ob-labs/powercontext-go/source"
)

func TestLiveOceanBaseProfileSmoke(t *testing.T) {
	url := os.Getenv("POWERCONTEXT_TEST_OCEANBASE_URL")
	if url == "" {
		t.Skip("set POWERCONTEXT_TEST_OCEANBASE_URL to a dedicated OceanBase MySQL-mode database")
	}
	config := sqlstore.OceanBaseConfig{URL: url, MaxOpenConns: 2, MaxIdleConns: 1}
	database, err := sqlstore.OpenOceanBase(context.Background(), config)
	if err != nil {
		var databaseError *mysql.MySQLError
		if errors.As(err, &databaseError) {
			t.Fatalf("OceanBase profile did not open: MySQL error %d (%s): %s", databaseError.Number, databaseError.SQLState, databaseError.Message)
		}
		t.Fatalf("OceanBase profile did not open: %T", err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	if err := database.Ping(context.Background()); err != nil {
		t.Fatal("OceanBase profile did not answer the compatibility probe")
	}

	ctx := t.Context()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	scopeID := "oceanbase-live-" + suffix
	if err := database.Transaction(ctx, func(tx sqlstore.DBTX) error {
		cursors := sqlstore.SourceCursorRepository{}
		stored, err := cursors.Save(ctx, tx, scopeID, "source_window", source.NewCursor(1), nil)
		if err != nil {
			return err
		}
		expected := stored.Generation
		_, err = cursors.Save(ctx, tx, scopeID, "source_window", source.NewCursor(2), &expected)
		return err
	}); err != nil {
		t.Fatalf("OceanBase Source cursor CAS failed: %T", err)
	}

	reports, err := sqlstore.NewHandoffReportStore(database, sqlstore.MySQLDialect)
	if err != nil {
		t.Fatal(err)
	}
	if err := reports.EnsureSchema(ctx); err != nil {
		t.Fatalf("OceanBase Handoff Report schema failed: %T", err)
	}
	projectID := "project-" + suffix
	project, err := handoffreport.NewProjectDescriptor(
		projectID, "live-"+suffix, "OceanBase live verification", nil,
		handoffreport.LocaleEnglish, "UTC", handoffreport.CatalogIncluded, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := reports.CreateProject(ctx, project, now); err != nil {
		t.Fatalf("OceanBase Handoff Report project failed: %T", err)
	}
	event, err := handoffreport.NewActivityEvent(handoffreport.ActivityEventInput{
		EventID: "event-" + suffix, ProjectID: projectID,
		Source: handoffreport.ActivityOther, SourceEventID: "source-event-" + suffix,
		ObservedAt: now, TimeBasis: handoffreport.TimeHostObserved,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reports.RecordActivity(ctx, event)
	if err != nil {
		t.Fatalf("OceanBase Handoff Report Activity failed: %T", err)
	}
	page, err := reports.ListActivities(ctx, projectID, nil, nil, nil, 0, nil, 10)
	if err != nil {
		t.Fatalf("OceanBase Handoff Report Activity page failed: %T", err)
	}
	if stored.Cursor != 1 || page.HighWatermark != 1 || len(page.Items) != 1 || page.Items[0].EventID() != event.EventID() {
		t.Fatalf("unexpected OceanBase Activity state: cursor=%d high=%d items=%d", stored.Cursor, page.HighWatermark, len(page.Items))
	}
}
