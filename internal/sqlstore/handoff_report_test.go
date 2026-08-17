package sqlstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thunguo/powercontext-go/handoffreport"
	"github.com/thunguo/powercontext-go/internal/sqlstore"
)

func TestHandoffReportSchemaIsOptInAndCatalogRevisionsAreAtomic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	var tables int
	if err := database.SQLDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
        WHERE type = 'table' AND name LIKE 'pc_handoff_report_%'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("disabled core created %d Handoff Report tables", tables)
	}
	store, err := sqlstore.NewHandoffReportStore(database, sqlstore.SQLiteDialect)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	project := reportProject(t, "prj-1", "one", 1, handoffreport.CatalogIncluded)
	if _, err := store.CreateProject(ctx, project, reportTime(1)); err != nil {
		t.Fatal(err)
	}
	workstream := reportWorkstream(t, "scope-a", "prj-1", 1, handoffreport.CatalogIncluded)
	if _, err := store.RegisterWorkstream(ctx, workstream, reportTime(1)); err != nil {
		t.Fatal(err)
	}
	updated := reportProject(t, "prj-1", "one", 2, handoffreport.CatalogArchived)
	if _, err := store.UpdateProject(ctx, updated, 1, reportTime(2)); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListProjects(ctx, nil, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("archived project leaked into default page: %#v", page.Items)
	}
	page, err = store.ListProjects(ctx, nil, 50, true)
	if err != nil || len(page.Items) != 1 || page.Items[0].Version() != 2 {
		t.Fatalf("all projects = %#v, %v", page, err)
	}
	var revisions int
	if err := database.SQLDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM pc_handoff_report_project_revisions WHERE project_id = ?", "prj-1").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 2 {
		t.Fatalf("revision count = %d", revisions)
	}
	stale := reportProject(t, "prj-1", "one", 2, handoffreport.CatalogIncluded)
	_, err = store.UpdateProject(ctx, stale, 1, reportTime(3))
	var conflict *handoffreport.ProjectConflictError
	if !errors.As(err, &conflict) || conflict.CurrentVersion == nil || *conflict.CurrentVersion != 2 {
		t.Fatalf("expected current version 2 conflict, got %v", err)
	}
	var version int
	if err := database.SQLDB().QueryRowContext(ctx, "SELECT version FROM pc_handoff_report_projects WHERE project_id = ?", "prj-1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("failed CAS changed project version to %d", version)
	}
}

func TestHandoffReportActivityIsGloballyIdempotentAndPurgeKeepsCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	store, _ := sqlstore.NewHandoffReportStore(database, sqlstore.SQLiteDialect)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	project := reportProject(t, "prj-1", "one", 1, handoffreport.CatalogIncluded)
	if _, err := store.CreateProject(ctx, project, reportTime(1)); err != nil {
		t.Fatal(err)
	}
	first := reportActivity(t, "evt-1", "prj-1", "git:stable", reportTime(2), reportTime(2).Add(-time.Hour), nil)
	stored, err := store.RecordActivity(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	retry := reportActivity(t, "evt-2", "prj-1", "git:stable", reportTime(2).Add(time.Minute), reportTime(2).Add(-time.Hour), nil)
	repeated, err := store.RecordActivity(ctx, retry)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Cursor != stored.Cursor || repeated.Event.EventID() != "evt-1" {
		t.Fatalf("idempotent retry = %#v", repeated)
	}
	changedTitle := "different"
	changed := reportActivity(t, "evt-3", "prj-1", "git:stable", reportTime(2), reportTime(2).Add(-time.Hour), &changedTitle)
	_, err = store.RecordActivity(ctx, changed)
	var eventConflict *handoffreport.ActivityEventConflictError
	if !errors.As(err, &eventConflict) {
		t.Fatalf("expected Activity conflict, got %v", err)
	}
	page, err := store.ListActivities(ctx, "prj-1", nil, nil, nil, 0, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.HighWatermark != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}
	deleted, err := store.PurgeActivities(ctx, "prj-1", reportTime(3))
	if err != nil || deleted != 1 {
		t.Fatalf("purge = %d, %v", deleted, err)
	}
	page, err = store.ListActivities(ctx, "prj-1", nil, nil, nil, 0, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.HighWatermark != 1 || len(page.Items) != 0 {
		t.Fatalf("purged page = %#v", page)
	}
}

func TestHandoffReportWorkspaceRequiresDetachBeforeProjectMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := openTestDatabase(t)
	store, _ := sqlstore.NewHandoffReportStore(database, sqlstore.SQLiteDialect)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	for _, value := range []handoffreport.ProjectDescriptor{reportProject(t, "prj-1", "one", 1, handoffreport.CatalogIncluded), reportProject(t, "prj-2", "two", 1, handoffreport.CatalogIncluded)} {
		if _, err := store.CreateProject(ctx, value, reportTime(1)); err != nil {
			t.Fatal(err)
		}
	}
	remote := "HTTPS://GitHub.com/oceanbase/powercontext.git/"
	subpath := "./services/api"
	repository, err := handoffreport.NewRepositoryRef(handoffreport.RepositoryGitHub, nil, &remote, &subpath)
	if err != nil {
		t.Fatal(err)
	}
	if got := *repository.NormalizedRemote(); got != "https://github.com/oceanbase/powercontext.git" {
		t.Fatalf("remote = %q", got)
	}
	if got := *repository.Subpath(); got != "services/api" {
		t.Fatalf("subpath = %q", got)
	}
	binding, err := store.AttachWorkspaceBinding(ctx, "workspace-1", "prj-1", repository, nil, reportTime(2))
	if err != nil || binding.Version() != 1 {
		t.Fatalf("attach = %#v, %v", binding, err)
	}
	expected := 1
	_, err = store.AttachWorkspaceBinding(ctx, "workspace-1", "prj-2", repository, &expected, reportTime(3))
	var conflict *handoffreport.WorkspaceBindingConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected project move conflict, got %v", err)
	}
	detached, err := store.DetachWorkspaceBinding(ctx, "workspace-1", 1)
	if err != nil || detached.State() != handoffreport.WorkspaceDetached {
		t.Fatalf("detach = %#v, %v", detached, err)
	}
	_, err = store.GetWorkspaceBinding(ctx, "workspace-1")
	var missing *handoffreport.WorkspaceBindingNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("detached binding should be hidden, got %v", err)
	}
	expected = 2
	rebound, err := store.AttachWorkspaceBinding(ctx, "workspace-1", "prj-2", repository, &expected, reportTime(4))
	if err != nil || rebound.Version() != 3 || rebound.ProjectID() != "prj-2" {
		t.Fatalf("rebind = %#v, %v", rebound, err)
	}
}

func reportProject(t *testing.T, id, key string, version int, state handoffreport.CatalogState) handoffreport.ProjectDescriptor {
	t.Helper()
	value, err := handoffreport.NewProjectDescriptor(id, key, "Project", nil, handoffreport.LocaleChinese, "UTC", state, version)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func reportWorkstream(t *testing.T, scope, project string, version int, state handoffreport.CatalogState) handoffreport.WorkstreamDescriptor {
	t.Helper()
	value, err := handoffreport.NewWorkstreamDescriptor(scope, project, nil, "Workstream", handoffreport.WorkstreamFeature, state, nil, nil, version)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func reportActivity(t *testing.T, id, project, sourceID string, observed, occurred time.Time, title *string) handoffreport.ActivityEvent {
	t.Helper()
	value, err := handoffreport.NewActivityEvent(handoffreport.ActivityEventInput{EventID: id, ProjectID: project, Source: handoffreport.ActivityGitCommit, SourceEventID: sourceID, OccurredAt: &occurred, ObservedAt: observed.UTC(), TimeBasis: handoffreport.TimeSourceReported, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func reportTime(day int) time.Time {
	return time.Date(2026, time.August, day, 10, 0, 0, 123456000, time.UTC)
}
