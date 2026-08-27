package handoffreport_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ob-labs/powercontext-go/artifact"
	"github.com/ob-labs/powercontext-go/artifact/handoff"
	"github.com/ob-labs/powercontext-go/internal/handoffreport"
	"github.com/ob-labs/powercontext-go/source"
)

func TestReportServiceAssemblesExactReadOnlyReportAndBilingualMarkdown(t *testing.T) {
	t.Parallel()
	selected := reportHandoff(t, 1)
	reader := &reportReader{values: map[string]*handoff.Handoff{"scope-a": &selected, "scope-b": nil}}
	report := generateServiceReport(t, reader, handoffreport.GenerateInput{
		Project: domainProject(t),
		Workstreams: []handoffreport.WorkstreamDescriptor{
			reportWorkstream(t, "scope-b", "Missing"),
			reportWorkstream(t, "scope-a", "Report"),
		},
		GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	selection, workstreams := report.EndSelection(), report.Workstreams()
	if len(selection) != 2 || selection[0].ScopeID() != "scope-a" || selection[1].ScopeID() != "scope-b" ||
		workstreams[0].Content() == nil || workstreams[1].ReportingStatus() != handoffreport.ReportingNoHandoff ||
		report.Coverage().ActivityCoverage != handoffreport.ActivityNotConfigured || reader.exactReads != 1 {
		t.Fatalf("report projection = selection:%#v workstreams:%#v coverage:%#v reads:%d", selection, workstreams, report.Coverage(), reader.exactReads)
	}
	chinese, err := handoffreport.RenderMarkdown(report)
	if err != nil {
		t.Fatal(err)
	}
	englishLocale := handoffreport.LocaleEnglish
	englishReader := &reportReader{values: map[string]*handoff.Handoff{"scope-a": &selected, "scope-b": nil}}
	englishReport := generateServiceReport(t, englishReader, handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: []handoffreport.WorkstreamDescriptor{
			reportWorkstream(t, "scope-a", "Report"), reportWorkstream(t, "scope-b", "Missing"),
		}, Locale: &englishLocale, GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	english, err := handoffreport.RenderMarkdown(englishReport)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chinese, "# PowerContext 项目交接报告") ||
		!strings.Contains(english, "# PowerContext Project Handoff Report") ||
		!strings.Contains(chinese, "Activity Adapter 未配置") ||
		!strings.Contains(chinese, "PowerContext &lt;script&gt;") ||
		!strings.Contains(chinese, `Add the API\.`) {
		t.Fatalf("unexpected bilingual Markdown\nChinese:\n%s\nEnglish:\n%s", chinese, english)
	}
}

func TestReportServiceCanSkipEvidenceChecksWithoutClaimingAvailability(t *testing.T) {
	t.Parallel()
	selected := reportHandoff(t, 1)
	reader := &reportReader{values: map[string]*handoff.Handoff{"scope-a": &selected}}
	report := generateServiceReport(t, reader, handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: []handoffreport.WorkstreamDescriptor{domainWorkstream(t, "scope-a")},
		IncludeEvidenceChecks: reportBool(false), GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	checks, checked := report.Workstreams()[0].EvidenceChecks()
	if checked || checks != nil || reader.evidenceReads != 0 {
		t.Fatalf("evidence checks = %#v, checked = %t, reads = %d", checks, checked, reader.evidenceReads)
	}
}

func TestReportServiceDegradesUnavailableEvidenceChecksPerWorkstream(t *testing.T) {
	t.Parallel()
	selected := reportHandoff(t, 1)
	reader := &reportReader{
		values: map[string]*handoff.Handoff{"scope-a": &selected}, evidenceUnavailable: true,
	}
	report := generateServiceReport(t, reader, handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: []handoffreport.WorkstreamDescriptor{domainWorkstream(t, "scope-a")},
		GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	item, coverage := report.Workstreams()[0], report.Coverage()
	_, checked := item.EvidenceChecks()
	if item.ReportingStatus() != handoffreport.ReportingEvidenceMissing || checked || !item.EvidenceUnavailable() ||
		coverage.UncheckedEvidenceWorkstreams != 1 || coverage.UnavailableEvidenceWorkstreams != 1 {
		t.Fatalf("item = %#v coverage = %#v", item, coverage)
	}
}

func TestReportServiceFailsClosedWhenExactReadDoesNotMatchFrozenSelection(t *testing.T) {
	t.Parallel()
	frozen, changed := reportHandoff(t, 1), reportHandoff(t, 2)
	base := &reportReader{values: map[string]*handoff.Handoff{"scope-a": &frozen}}
	reader := &inconsistentReportReader{reportReader: base, exact: changed}
	service, err := handoffreport.NewService(reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Generate(context.Background(), handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: []handoffreport.WorkstreamDescriptor{domainWorkstream(t, "scope-a")},
		GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	if inconsistent, ok := err.(*handoffreport.InconsistentError); !ok || inconsistent.ScopeID != "scope-a" {
		t.Fatalf("error = %#v, want scope-a InconsistentError", err)
	}
}

func TestReportServiceEnforcesSelectionLimitsBeforeReadingHandoffs(t *testing.T) {
	t.Parallel()
	reader := &reportReader{values: map[string]*handoff.Handoff{}}
	service, err := handoffreport.NewService(reader)
	if err != nil {
		t.Fatal(err)
	}
	generated := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	workstreams := make([]handoffreport.WorkstreamDescriptor, handoffreport.MaxReportWorkstreams+1)
	for index := range workstreams {
		workstreams[index] = reportWorkstream(t, "scope-"+decimal(index), "Workstream "+decimal(index))
	}
	if _, err := service.Generate(context.Background(), handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: workstreams, GeneratedAt: generated,
	}); err == nil {
		t.Fatal("report accepted too many Workstreams")
	}
	event := reportActivity(t, "event", nil, nil, nil)
	activities := make([]handoffreport.ActivityEvent, handoffreport.MaxReportActivities+1)
	for index := range activities {
		activities[index] = event
	}
	if _, err := service.Generate(context.Background(), handoffreport.GenerateInput{
		Project: domainProject(t), Activities: activities, GeneratedAt: generated,
	}); err == nil {
		t.Fatal("report accepted too many Activity Events")
	}
	if _, err := service.Generate(context.Background(), handoffreport.GenerateInput{
		Project: domainProject(t), ActivityCursor: -1, GeneratedAt: generated,
	}); err == nil {
		t.Fatal("report accepted a negative activity cursor")
	}
	if reader.latestReads != 0 || reader.exactReads != 0 {
		t.Fatalf("reader was called before limit validation: latest=%d exact=%d", reader.latestReads, reader.exactReads)
	}
}

func TestReportMarkdownCarriesExactSelectionCursorAndAllActivity(t *testing.T) {
	t.Parallel()
	selected := reportHandoff(t, 1)
	scope, assignedTitle, unassignedTitle := "scope-a", "Assigned event", "Unassigned event"
	report := generateServiceReport(t, &reportReader{values: map[string]*handoff.Handoff{"scope-a": &selected}}, handoffreport.GenerateInput{
		Project: domainProject(t), Workstreams: []handoffreport.WorkstreamDescriptor{domainWorkstream(t, scope)},
		Activities: []handoffreport.ActivityEvent{
			reportActivity(t, "evt-assigned", &scope, &assignedTitle, nil),
			reportActivity(t, "evt-unassigned", nil, &unassignedTitle, nil),
		},
		ActivityCursor: 7, ActivityCoverage: handoffreport.ActivityCaptured,
		GeneratedAt: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	markdown, err := handoffreport.RenderMarkdown(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`project_id: "prj-1"`, "project_version: 1", "activity_cursor: 7", "end_selection:",
		`scope_id: "scope-a"`, "workstream_revision: 1", `family: "handoff"`,
		`artifact_id: "handoff"`, "revision: 1", "activity_selection:",
		`  - "evt-assigned"`, `  - "evt-unassigned"`, "#### 观察到的 Activity",
		"Assigned event", "## 未分配 Activity", "Unassigned event", "来源事件 ID", "信任标记",
	} {
		if !strings.Contains(markdown, fragment) {
			t.Fatalf("Markdown lacks %q:\n%s", fragment, markdown)
		}
	}
	again, err := handoffreport.RenderMarkdown(report)
	if err != nil || again != markdown {
		t.Fatal("Markdown rendering is not deterministic")
	}
}

func TestReportMarkdownEscapesUserTextAndUsesSafeBacktickDelimiters(t *testing.T) {
	t.Parallel()
	project, err := handoffreport.NewProjectDescriptor(
		"prj-1", "powercontext", "Project [link](https://evil)\n![image](x)<script>alert(1)</script>",
		nil, handoffreport.LocaleChinese, "Asia/Shanghai", handoffreport.CatalogIncluded, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	scope := "scope`tick"
	workstream := reportWorkstream(t, scope, "Feature # heading\n[click](javascript:evil)")
	selected := reportHandoffWithText(
		t, "Objective **bold**\n![objective](https://evil)",
		"State <img src=x onerror=alert(1)>\nsecond line", "Next [link](https://evil)",
	)
	provider, externalID, target := "host`provider", "issue](javascript:evil)", "https://evil/`](x)"
	reference, err := handoffreport.NewExternalReference(handoffreport.ExternalIssue, provider, externalID, &target)
	if err != nil {
		t.Fatal(err)
	}
	agentProvider, agentLabel := "codex`host", "agent [admin]"
	agent, err := handoffreport.NewActivityAgent(&agentProvider, &agentLabel)
	if err != nil {
		t.Fatal(err)
	}
	branch, head := "feature/[link]", "abc`123"
	vcs, err := handoffreport.NewActivityVCSContext(&branch, &head)
	if err != nil {
		t.Fatal(err)
	}
	title, summary, session := "Activity ![image](x)\n<title>", "Summary [link](javascript:evil)\nnext line", "session`id"
	activity := reportActivity(t, "evt`assigned", &scope, &title, func(input *handoffreport.ActivityEventInput) {
		input.Summary = &summary
		input.SourceRef = &reference
		input.EvidenceRefs = []handoffreport.ExternalReference{reference}
		input.Agent, input.SessionID, input.VCSContext = &agent, &session, &vcs
	})
	report := generateServiceReport(t, &reportReader{values: map[string]*handoff.Handoff{scope: &selected}}, handoffreport.GenerateInput{
		Project: project, Workstreams: []handoffreport.WorkstreamDescriptor{workstream},
		Activities: []handoffreport.ActivityEvent{activity}, ActivityCursor: 1,
		ActivityCoverage: handoffreport.ActivityCaptured,
		GeneratedAt:      time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
	})
	markdown, err := handoffreport.RenderMarkdown(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<script>", "<img", "![image](x)", "[link](https://evil)"} {
		if strings.Contains(markdown, forbidden) {
			t.Fatalf("Markdown contains unsafe user syntax %q:\n%s", forbidden, markdown)
		}
	}
	for _, escaped := range []string{
		"&lt;script&gt;", `\!\[image\]\(x\)`,
		`Project \[link\]\(https://evil\) \!\[image\]\(x\)&lt;script&gt;`,
		`State &lt;img src=x onerror=alert\(1\)&gt; second line`,
		`Summary \[link\]\(javascript:evil\) next line`,
		"`` scope`tick ``", "`` evt`assigned ``", "`` session`id ``",
	} {
		if !strings.Contains(markdown, escaped) {
			t.Fatalf("Markdown lacks escaped value %q:\n%s", escaped, markdown)
		}
	}
}

type inconsistentReportReader struct {
	*reportReader
	exact handoff.Handoff
}

func (r *inconsistentReportReader) Get(context.Context, string, artifact.Ref) (handoff.Handoff, error) {
	r.exactReads++
	return r.exact, nil
}

func generateServiceReport(t *testing.T, reader handoffreport.HandoffReader, input handoffreport.GenerateInput) handoffreport.Report {
	t.Helper()
	service, err := handoffreport.NewService(reader)
	if err != nil {
		t.Fatal(err)
	}
	value, err := service.Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func reportWorkstream(t *testing.T, scope, title string) handoffreport.WorkstreamDescriptor {
	t.Helper()
	value, err := handoffreport.NewWorkstreamDescriptor(
		scope, "prj-1", nil, title, handoffreport.WorkstreamFeature,
		handoffreport.CatalogIncluded, nil, nil, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func reportActivity(
	t *testing.T,
	id string,
	scope *string,
	title *string,
	update func(*handoffreport.ActivityEventInput),
) handoffreport.ActivityEvent {
	t.Helper()
	occurred := time.Date(2026, time.August, 5, 1, 0, 0, 0, time.UTC)
	input := handoffreport.ActivityEventInput{
		EventID: id, ProjectID: "prj-1", ScopeID: scope,
		Source: handoffreport.ActivityCodingSession, SourceEventID: "source-" + id,
		OccurredAt: &occurred, ObservedAt: occurred.Add(time.Hour), TimeBasis: handoffreport.TimeSourceReported,
		Title: title,
	}
	if update != nil {
		update(&input)
	}
	value, err := handoffreport.NewActivityEvent(input)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func reportHandoffWithText(t *testing.T, objective, stateText, nextText string) handoff.Handoff {
	t.Helper()
	ref, err := source.NewRef(source.ContentType, "source-1")
	if err != nil {
		t.Fatal(err)
	}
	citation, err := handoff.NewSourceCitation(ref)
	if err != nil {
		t.Fatal(err)
	}
	state, err := handoff.NewStatement(stateText, []handoff.Citation{citation})
	if err != nil {
		t.Fatal(err)
	}
	next, err := handoff.NewStatement(nextText, []handoff.Citation{citation})
	if err != nil {
		t.Fatal(err)
	}
	content, err := handoff.NewContent(objective, []handoff.Statement{state}, handoff.Continuable, &next, nil)
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

func decimal(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
