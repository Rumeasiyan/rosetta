package usage

import (
	"context"
	"log/slog"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/jackc/pgx/v5"
)

// Nightly is PARADOX's nightly job (R-REC-04; B 12): add up the records, then remove what has
// reached its time (R-NFR-12). It all stands in one transaction, so a failure removes nothing, and
// totals are written before any record they count can go. It reports the rows it touched.
func Nightly(ctx context.Context, tx pgx.Tx, v settings.Values, logger *slog.Logger, now time.Time) (int, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	totals, err := AddTotals(ctx, tx)
	if err != nil {
		return 0, err
	}
	months, err := DropOldParts(ctx, tx, v.Of(settings.UsageRecordYears), now)
	if err != nil {
		return 0, err
	}
	oldTotals, err := DropOldTotals(ctx, tx, v.Of(settings.DailyTotalYears))
	if err != nil {
		return 0, err
	}
	removed := map[string]string{
		"cache":  `DELETE FROM cache_piece WHERE expires_at < now()`,
		"alerts": `DELETE FROM alert WHERE raised_at < now() - make_interval(days => 90)`,
		"runs":   `DELETE FROM paradox_run WHERE started_at < now() - make_interval(days => 90)`,
	}
	gone := map[string]int{}
	for name, sql := range removed {
		tag, err := tx.Exec(ctx, sql)
		if err != nil {
			return 0, err
		}
		gone[name] = int(tag.RowsAffected())
	}
	outbox, err := audit.DropExpired(ctx, tx, logger, now)
	if err != nil {
		return 0, err
	}
	logger.Info("nightly work", "totals", totals, "months_dropped", months, "totals_dropped", oldTotals,
		"cache_removed", gone["cache"], "alerts_removed", gone["alerts"], "runs_removed", gone["runs"], "outbox_removed", outbox)
	return totals + months + oldTotals + gone["cache"] + gone["alerts"] + gone["runs"] + outbox, nil
}
