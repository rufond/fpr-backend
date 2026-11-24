package portfolio

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/routes"
	"github.com/rufond/fpr-backend/internal/validationrules"
	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"
)

const MaxImportBytes = 2 << 20

type Handler struct {
	archive     atomic.Pointer[Archive]
	db          *pgxpool.Pool
	state       *appstate.Manager
	publisher   realtime.Publisher
	officialURL string
}

func NewHandler(db *pgxpool.Pool, state *appstate.Manager, publisher realtime.Publisher, officialURL string) *Handler {
	return &Handler{db: db, state: state, publisher: publisher, officialURL: officialURL}
}

func (h *Handler) Start(ctx context.Context) error {
	archive, err := loadArchive(ctx, h.db)
	if err != nil {
		return err
	}

	h.archive.Store(archive)
	return nil
}

func (h *Handler) Import(request routes.Request) (int, error, any) {
	reports, errRead := ReadReports(bytes.NewReader(request.RawBody))
	if errRead != nil {
		return http.StatusUnprocessableEntity, nil, map[string]string{"error": errRead.Error()}
	}
	if len(reports) == 0 {
		return http.StatusUnprocessableEntity, nil, map[string]string{"error": "В файле нет справок."}
	}

	ctx := request.Context
	var inserted int64
	errRead = h.state.Update(func(current *appstate.State) (*appstate.State, error) {
		tx, err := h.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		inserted, err = insertReports(ctx, tx, reports)
		if err != nil || inserted == 0 {
			return current, err
		}

		archive, err := loadArchive(ctx, tx)
		if err != nil {
			return nil, err
		}

		count, err := fund.LoadUnitCount(ctx, tx, current.Fund.Snapshot.AsOfDate)
		if err != nil {
			return nil, err
		}

		if errCommit := tx.Commit(ctx); errCommit != nil {
			return nil, errCommit
		}

		next := new(*current)
		next.Fund = new(*current.Fund)
		next.Fund.Snapshot.UnitCount = count
		h.archive.Store(archive)

		return next, nil
	})
	if errRead != nil {
		return http.StatusInternalServerError, errRead, map[string]string{"error": "Не удалось сохранить справки."}
	}

	if inserted > 0 {
		h.publisher.Publish(realtime.Update{Scopes: []string{realtime.ScopePortfolio, realtime.ScopeFundState}})
	}

	return http.StatusOK, nil, map[string]int64{"imported": inserted, "skipped": int64(len(reports)) - inserted}
}

func (h *Handler) Entry(isin, name, asOf string) (IPO, bool) {
	archive := h.archive.Load()
	if archive == nil {
		return IPO{}, false
	}

	return archive.Entry(isin, name, asOf)
}

func (h *Handler) Index(_ routes.Request) (int, error, any) {
	return http.StatusOK, nil, h.archive.Load().Index()
}

func (h *Handler) Closed(_ routes.Request) (int, error, any) {
	state := h.state.Load()
	if state == nil || state.Fund == nil {
		return http.StatusServiceUnavailable, nil, map[string]string{"error": "fund state is not available"}
	}

	return http.StatusOK, nil, h.archive.Load().Closed(state, h.officialURL)
}

type dateRequest struct {
	Date string `json:"as_of_date"`
}

func (h *Handler) Snapshot(request routes.Request) (int, error, any) {
	input, errors := govalidate.Run[dateRequest](request.Body, map[string][]any{
		"as_of_date": {rules.String{}, rules.Required{}},
	})
	if len(errors) != 0 || !validDate(input.Date) {
		return http.StatusUnprocessableEntity, nil, map[string]string{"error": "invalid as_of_date, expected YYYY-MM-DD"}
	}

	result, found := h.archive.Load().Snapshot(input.Date)
	if !found {
		return http.StatusNotFound, nil, map[string]string{"error": "report not found"}
	}

	return http.StatusOK, nil, result
}

func (h *Handler) Changes(request routes.Request) (int, error, any) {
	input, errors := govalidate.Run[dateRequest](request.Body, map[string][]any{
		"as_of_date": {rules.String{}},
	})
	if len(errors) != 0 || (input.Date != "" && !validDate(input.Date)) {
		return http.StatusUnprocessableEntity, nil, map[string]string{"error": "invalid as_of_date, expected YYYY-MM-DD"}
	}

	changes := h.archive.Load().Changes(h.state.Load(), h.officialURL)
	if input.Date != "" {
		changes = slices.DeleteFunc(changes, func(item Change) bool { return item.ToDate != input.Date })
	}

	return http.StatusOK, nil, changes
}

type instrumentRequest struct {
	ISIN string `json:"isin"`
}

func (h *Handler) Detail(request routes.Request) (int, error, any) {
	input, errors := govalidate.Run[instrumentRequest](request.Body, map[string][]any{
		"isin": {rules.Required{}, validationrules.ISIN{}},
	})
	if len(errors) != 0 {
		return http.StatusUnprocessableEntity, nil, map[string]any{"errors": errors}
	}

	return http.StatusOK, nil, h.archive.Load().Detail(input.ISIN, h.state.Load(), h.officialURL)
}

func (h *Handler) IPOs(_ routes.Request) (int, error, any) {
	return http.StatusOK, nil, h.archive.Load().IPOs()
}

func validDate(value string) bool {
	_, err := time.Parse(time.DateOnly, value)
	return err == nil
}
