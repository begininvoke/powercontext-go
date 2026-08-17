package sqlstore

import (
	"context"
	"time"

	"github.com/thunguo/powercontext-go/handoffreport"
)

// ReadHandoffReportInputs freezes the mutable catalog and Activity journal in
// one transaction. Handoff head selection intentionally happens afterwards so
// no model or cross-scope read is ever performed while this transaction lives.
func (s *HandoffReportStore) ReadHandoffReportInputs(
	ctx context.Context,
	projectID string,
	includeArchived bool,
	periodStart, periodEnd, previousStart, previousEnd *time.Time,
) (
	project handoffreport.ProjectDescriptor,
	workstreams []handoffreport.WorkstreamDescriptor,
	activities []handoffreport.ActivityEvent,
	activityCursor int64,
	previousActivityCount int,
	err error,
) {
	err = s.database.Transaction(ctx, func(tx DBTX) error {
		project, err = s.getProject(ctx, tx, projectID)
		if err != nil {
			return err
		}
		page, readErr := s.listWorkstreams(ctx, tx, projectID, nil, handoffreport.MaxReportWorkstreams, includeArchived)
		if readErr != nil {
			return readErr
		}
		if page.NextCursor != nil {
			return &handoffreport.TooLargeError{SelectedWorkstreams: handoffreport.MaxReportWorkstreams + 1}
		}
		workstreams = page.Items
		activityCursor, readErr = s.activityHighWatermark(ctx, tx, projectID)
		if readErr != nil {
			return readErr
		}
		stored, readErr := s.listActivityRows(ctx, tx, projectID, periodStart, periodEnd, nil, 0, &activityCursor, handoffreport.MaxReportActivities+1)
		if readErr != nil {
			return readErr
		}
		if len(stored) > handoffreport.MaxReportActivities {
			return &handoffreport.TooLargeError{SelectedWorkstreams: len(workstreams), SelectedActivities: len(stored)}
		}
		activities = make([]handoffreport.ActivityEvent, len(stored))
		for index, item := range stored {
			activities[index] = item.Event
		}
		if previousStart != nil || previousEnd != nil {
			previous, previousErr := s.listActivityRows(ctx, tx, projectID, previousStart, previousEnd, nil, 0, &activityCursor, handoffreport.MaxReportActivities+1)
			if previousErr != nil {
				return previousErr
			}
			if len(previous) > handoffreport.MaxReportActivities {
				return &handoffreport.TooLargeError{SelectedWorkstreams: len(workstreams), SelectedActivities: len(previous)}
			}
			previousActivityCount = len(previous)
		}
		return nil
	})
	return
}
