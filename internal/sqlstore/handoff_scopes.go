package sqlstore

import (
	"context"

	"github.com/ob-labs/powercontext-go/artifact/handoff"
)

// HandoffScopeIDs returns only scopes that own a committed Handoff head. It is
// the authority behind scope-centric Handoff Report discovery.
func HandoffScopeIDs(ctx context.Context, database *Database) ([]string, error) {
	var result []string
	err := database.Transaction(ctx, func(tx DBTX) error {
		rows, err := tx.QueryContext(ctx,
			"SELECT DISTINCT scope_id FROM pc_artifact_heads WHERE family = ? ORDER BY scope_id", handoff.Family,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scope string
			if err := rows.Scan(&scope); err != nil {
				return err
			}
			result = append(result, scope)
		}
		return rows.Err()
	})
	return result, err
}
