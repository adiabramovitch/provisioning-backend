package services

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/RHEnVision/provisioning-backend/internal/ctxval"
	"github.com/RHEnVision/provisioning-backend/internal/dao"
	"github.com/RHEnVision/provisioning-backend/internal/jobs"
	"github.com/RHEnVision/provisioning-backend/internal/models"
	"github.com/RHEnVision/provisioning-backend/internal/payloads"
	"github.com/go-chi/render"
)

var InvalidRequestPubkeyNewError = errors.New("provide either existing (via NewName/NewBody) or new pubkey (ExistingID)")
var InvalidRequestPubkeyMissingError = errors.New("provide both NewName and NewBody for pubkey")

func CreateReservation(w http.ResponseWriter, r *http.Request) {
	logger := ctxval.Logger(r.Context())

	// TODO: get this from X-RH-Identity via middleware/context
	var accountId int64 = 1

	payload := &payloads.ReservationRequest{}
	if err := render.Bind(r, payload); err != nil {
		renderError(w, r, payloads.NewInvalidRequestError(r.Context(), err))
		return
	}

	rDao, err := dao.GetReservationDao(r.Context())
	if err != nil {
		renderError(w, r, payloads.NewInitializeDAOError(r.Context(), "reservation DAO", err))
		return
	}
	pkDao, err := dao.GetPubkeyDao(r.Context())
	if err != nil {
		renderError(w, r, payloads.NewInitializeDAOError(r.Context(), "pubkey DAO", err))
		return
	}
	pkrDao, err := dao.GetPubkeyResourceDao(r.Context())
	if err != nil {
		renderError(w, r, payloads.NewInitializeDAOError(r.Context(), "pubkey resource DAO", err))
		return
	}

	// validate pubkey
	if payload.Provider != models.ProviderTypeNoop {
		if payload.Pubkey.ExistingID == nil &&
			payload.Pubkey.NewName == nil &&
			payload.Pubkey.NewBody == nil {
			renderError(w, r, payloads.NewInvalidRequestError(r.Context(), InvalidRequestPubkeyNewError))
		}
		if payload.Pubkey.ExistingID != nil &&
			payload.Pubkey.NewName != nil &&
			payload.Pubkey.NewBody != nil {
			renderError(w, r, payloads.NewInvalidRequestError(r.Context(), InvalidRequestPubkeyNewError))
		}
		if payload.Pubkey.ExistingID == nil && (payload.Pubkey.NewName == nil || payload.Pubkey.NewBody == nil) {
			renderError(w, r, payloads.NewInvalidRequestError(r.Context(), InvalidRequestPubkeyMissingError))
		}
	}

	reservation := &models.Reservation{
		Provider:  payload.Provider,
		AccountID: accountId,
		Status:    "Created",
	}

	// create or validate pubkey
	var pk *models.Pubkey
	if payload.Provider != models.ProviderTypeNoop {
		if payload.Pubkey.ExistingID == nil {
			pk = &models.Pubkey{
				AccountID: accountId,
				Name:      *payload.Pubkey.NewName,
				Body:      *payload.Pubkey.NewBody,
			}
			err = pkDao.Create(r.Context(), pk)
			if err != nil {
				renderError(w, r, payloads.NewDAOError(r.Context(), "create pubkey", err))
				return
			}
			logger.Debug().Msgf("Created a new pubkey %d named '%s'", pk.ID, pk.Name)
		} else {
			// TODO: IMPORTANT! Must perform GetByIdAndAccount to prevent pubkey hijack
			logger.Debug().Msgf("Validating existing pubkey %d", *payload.Pubkey.ExistingID)
			pk, err = pkDao.GetById(r.Context(), *payload.Pubkey.ExistingID)
			if err != nil {
				var e *dao.NoRowsError
				if errors.As(err, &e) {
					renderError(w, r, payloads.NewNotFoundError(r.Context(), err))
				} else {
					renderError(w, r, payloads.NewDAOError(r.Context(), "get pubkey by id", err))
				}
				return
			}
			logger.Debug().Msgf("Found pubkey %d named '%s'", pk.ID, pk.Name)

		}

		reservation.PubkeyID = sql.NullInt64{Int64: pk.ID, Valid: true}
	}

	// create reservation in the database
	err = rDao.Create(r.Context(), reservation)
	if err != nil {
		renderError(w, r, payloads.NewDAOError(r.Context(), "create reservation", err))
		return
	}
	logger.Debug().Msgf("Created a new reservation %d", reservation.ID)

	// find existing resource
	if payload.Provider != models.ProviderTypeNoop {
		uploadNeeded := false
		pkr, err := pkrDao.GetResourceByProviderType(r.Context(), pk.ID, payload.Provider)
		if err != nil {
			var e *dao.NoRowsError
			if errors.As(err, &e) {
				uploadNeeded = true
			} else {
				renderError(w, r, payloads.NewDAOError(r.Context(), "get pubkey by id", err))
				return
			}
		}

		// enqueue upload job if the key was not uploaded yet
		if uploadNeeded {
			if payload.Provider == models.ProviderTypeAWS {
				logger.Debug().Msgf("Enqueuing upload key job for pubkey %d", pk.ID)
				args := &jobs.PubkeyUploadAWSTaskArgs{
					AccountID:     accountId,
					ReservationID: reservation.ID,
					PubkeyID:      pk.ID,
				}
				err = jobs.EnqueuePubkeyUploadAWS(r.Context(), args)
				if err != nil {
					renderError(w, r, payloads.NewEnqueueTaskError(r.Context(), "EnqueuePubkeyUploadAWS", err))
					return
				}
			} else {
				logger.Warn().Msg("Provider type not implemented yet")
			}
		} else {
			logger.Debug().Msgf("Found existing pubkey resource %d, upload not enqueued", pkr.ID)
		}
	} else {
		// noop job
		args := &jobs.NoopJobArgs{
			AccountID:     accountId,
			ReservationID: reservation.ID,
		}
		err = jobs.EnqueueNoop(r.Context(), args)
		if err != nil {
			renderError(w, r, payloads.NewEnqueueTaskError(r.Context(), "EnqueueNoop", err))
			return
		}
	}

	if err := render.Render(w, r, payloads.NewReservationResponse(reservation)); err != nil {
		renderError(w, r, payloads.NewRenderError(r.Context(), "reservation", err))
	}
}

func ListReservations(w http.ResponseWriter, r *http.Request) {
	rDao, err := dao.GetReservationDao(r.Context())
	if err != nil {
		renderError(w, r, payloads.NewInitializeDAOError(r.Context(), "reservation DAO", err))
		return
	}

	reservations, err := rDao.List(r.Context(), 100, 0)
	if err != nil {
		renderError(w, r, payloads.NewDAOError(r.Context(), "list reservations", err))
		return
	}

	if err := render.RenderList(w, r, payloads.NewReservationListResponse(reservations)); err != nil {
		renderError(w, r, payloads.NewRenderError(r.Context(), "list reservations", err))
		return
	}
}
