package handoffreport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/source"
)

func TestCanonicalReportRejectsWrongProjectRevisionAndHandoffProjection(t *testing.T) {
	t.Parallel()
	t.Run("workstream project", func(t *testing.T) {
		report := validReportForMutation(t)
		report.workstreams[0].workstream.projectID = "prj-other"
		assertInvalidReport(t, report, "Report Project")
	})
	t.Run("workstream revision", func(t *testing.T) {
		report := validReportForMutation(t)
		report.endSelection[0].workstreamRevision++
		assertInvalidReport(t, report, "Workstream revision")
	})
	t.Run("handoff projection", func(t *testing.T) {
		report := validReportForMutation(t)
		wrong, err := artifact.NewRef(handoff.Family, "different", 1)
		if err != nil {
			t.Fatal(err)
		}
		report.workstreams[0].handoffRef = &wrong
		assertInvalidReport(t, report, "Handoff reference")
	})
}

func TestCanonicalReportRejectsDuplicateScopesAndActivityIDs(t *testing.T) {
	t.Parallel()
	t.Run("scope", func(t *testing.T) {
		report := validReportForMutation(t)
		report.endSelection = append(report.endSelection, report.endSelection[0])
		report.workstreams = append(report.workstreams, report.workstreams[0])
		report.coverage.SelectedWorkstreams = 2
		assertInvalidReport(t, report, "selection scopes must be unique")
	})
	t.Run("activity id", func(t *testing.T) {
		report := validReportForMutation(t)
		assignedID := report.workstreams[0].activities[0].eventID
		report.unassignedActivity[0].eventID = assignedID
		report.activitySelection = []string{assignedID, assignedID}
		assertInvalidReport(t, report, "Activity Event ids must be unique")
	})
}

func TestCanonicalReportRejectsInvalidAssignedActivityOwnership(t *testing.T) {
	t.Parallel()
	t.Run("project", func(t *testing.T) {
		report := validReportForMutation(t)
		report.workstreams[0].activities[0].projectID = "prj-other"
		assertInvalidReport(t, report, "Report Project")
	})
	t.Run("scope", func(t *testing.T) {
		report := validReportForMutation(t)
		other := "scope-other"
		report.workstreams[0].activities[0].scopeID = &other
		assertInvalidReport(t, report, "scope must match")
	})
}

func TestCanonicalReportRejectsInvalidUnassignedActivityAndSelection(t *testing.T) {
	t.Parallel()
	t.Run("project", func(t *testing.T) {
		report := validReportForMutation(t)
		report.unassignedActivity[0].projectID = "prj-other"
		assertInvalidReport(t, report, "Report Project")
	})
	t.Run("selected scope", func(t *testing.T) {
		report := validReportForMutation(t)
		selected := "scope-a"
		report.unassignedActivity[0].scopeID = &selected
		assertInvalidReport(t, report, "cannot be unassigned")
	})
	t.Run("selection", func(t *testing.T) {
		report := validReportForMutation(t)
		report.activitySelection = report.activitySelection[:1]
		assertInvalidReport(t, report, "activity_selection")
	})
	t.Run("coverage", func(t *testing.T) {
		report := validReportForMutation(t)
		report.coverage.UnassignedActivityCount = 0
		assertInvalidReport(t, report, "coverage")
	})
}

func assertInvalidReport(t *testing.T, report Report, fragment string) {
	t.Helper()
	err := report.Validate()
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("Validate() error = %v, want text containing %q", err, fragment)
	}
}

func validReportForMutation(t *testing.T) Report {
	t.Helper()
	handoffValue := validationHandoff(t)
	service, err := NewService(validationReportReader{value: handoffValue})
	if err != nil {
		t.Fatal(err)
	}
	project, err := NewProjectDescriptor(
		"prj-1", "powercontext", "PowerContext", nil,
		LocaleChinese, "Asia/Shanghai", CatalogIncluded, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	workstream, err := NewWorkstreamDescriptor(
		"scope-a", "prj-1", nil, "Report", WorkstreamFeature,
		CatalogIncluded, nil, nil, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	assignedScope := "scope-a"
	assigned := validationActivity(t, "evt-assigned", &assignedScope)
	unassigned := validationActivity(t, "evt-unassigned", nil)
	report, err := service.Generate(context.Background(), GenerateInput{
		Project: project, Workstreams: []WorkstreamDescriptor{workstream},
		Activities: []ActivityEvent{assigned, unassigned}, ActivityCursor: 7,
		ActivityCoverage: ActivityCaptured,
		GeneratedAt:      time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func validationActivity(t *testing.T, id string, scopeID *string) ActivityEvent {
	t.Helper()
	occurred := time.Date(2026, time.August, 5, 1, 0, 0, 0, time.UTC)
	value, err := NewActivityEvent(ActivityEventInput{
		EventID: id, ProjectID: "prj-1", ScopeID: scopeID,
		Source: ActivityCodingSession, SourceEventID: "source-" + id,
		OccurredAt: &occurred, ObservedAt: occurred.Add(time.Hour), TimeBasis: TimeSourceReported,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func validationHandoff(t *testing.T) handoff.Handoff {
	t.Helper()
	ref, err := source.NewRef(source.ContentType, "source-1")
	if err != nil {
		t.Fatal(err)
	}
	citation, err := handoff.NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	state, err := handoff.NewStatement("The model exists.", []handoff.Citation{citation})
	if err != nil {
		t.Fatal(err)
	}
	next, err := handoff.NewStatement("Add the API.", []handoff.Citation{citation})
	if err != nil {
		t.Fatal(err)
	}
	content, err := handoff.NewContent("Implement the report.", []handoff.Statement{state}, handoff.Continuable, &next, nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := handoff.NewArtifactDraft(content, []source.Ref{ref}, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := artifact.New("handoff", 1, draft)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type validationReportReader struct{ value handoff.Handoff }

func (r validationReportReader) Latest(context.Context, string) (handoff.Handoff, bool, error) {
	return r.value, true, nil
}

func (r validationReportReader) Get(context.Context, string, artifact.Ref) (handoff.Handoff, error) {
	return r.value, nil
}

func (r validationReportReader) Revisions(context.Context, string) ([]handoff.Handoff, error) {
	return []handoff.Handoff{r.value}, nil
}

func (validationReportReader) CheckEvidence(context.Context, string, artifact.Ref) ([]handoff.EvidenceCheck, error) {
	return []handoff.EvidenceCheck{}, nil
}
