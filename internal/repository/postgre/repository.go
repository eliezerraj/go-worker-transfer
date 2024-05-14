package postgre

import (
	"context"
	"errors"
	"time"
	"database/sql"
	
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"

	"github.com/go-worker-transfer/internal/core"
	"go.opentelemetry.io/otel"

)

var childLogger = log.With().Str("repository", "WorkerRepository").Logger()

type WorkerRepository struct {
	databaseHelper DatabaseHelper
}

func NewWorkerRepository(databaseHelper DatabaseHelper) WorkerRepository {
	childLogger.Debug().Msg("NewWorkerRepository")
	return WorkerRepository{
		databaseHelper: databaseHelper,
	}
}

func (w WorkerRepository) StartTx(ctx context.Context) (*sql.Tx, error) {
	childLogger.Debug().Msg("StartTx")

	client := w.databaseHelper.GetConnection()

	tx, err := client.BeginTx(ctx, &sql.TxOptions{})
    if err != nil {
        return nil, errors.New(err.Error())
    }

	return tx, nil
}

func (w WorkerRepository) Ping(ctx context.Context) (bool, error) {
	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")
	childLogger.Debug().Msg("Ping")
	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")

	client := w.databaseHelper.GetConnection()

	err := client.PingContext(ctx)
	if err != nil {
		return false, errors.New(err.Error())
	}

	return true, nil
}

func (w WorkerRepository) Update(ctx context.Context, tx *sql.Tx, transfer core.Transfer) (int64, error){
	childLogger.Debug().Msg("Update")
	childLogger.Debug().Interface("transfer : ", transfer).Msg("")

	ctx, repospan := otel.Tracer("go-worker-transfer").Start(ctx,"repo.update")
	defer repospan.End()

	stmt, err := tx.Prepare(`Update transfer_moviment
									set status = $2
								where id = $1 `)
	if err != nil {
		childLogger.Error().Err(err).Msg("UPDATE statement")
		return 0, errors.New(err.Error())
	}

	result, err := stmt.ExecContext(ctx,	
									transfer.ID,
									transfer.Status,
								)
	if err != nil {
		childLogger.Error().Err(err).Msg("Exec statement")
		return 0, errors.New(err.Error())
	}

	rowsAffected, _ := result.RowsAffected()
	childLogger.Debug().Int("rowsAffected : ",int(rowsAffected)).Msg("")

	defer stmt.Close()
	return rowsAffected , nil
}

func (w WorkerRepository) AddTransferMoviment(ctx context.Context, tx *sql.Tx ,transfer core.Transfer) (*core.Transfer, error){
	childLogger.Debug().Msg("AddTransferMoviment")
	childLogger.Debug().Interface("transfer:",transfer).Msg("")

	ctx, repospan := otel.Tracer("go-worker-transfer").Start(ctx,"repo.addTransferMoviment")
	defer repospan.End()

	stmt, err := tx.Prepare(`INSERT INTO transfer_moviment ( 	fk_account_id_from, 
																fk_account_id_to,
																type_charge,
																status,  
																transfer_at,
																currency,
																amount) 
									VALUES($1, $2, $3, $4, $5, $6, $7) RETURNING id`)
	if err != nil {
		childLogger.Error().Err(err).Msg("INSERT statement")
		return nil, errors.New(err.Error())
	}

	var id int
	err = stmt.QueryRowContext(	ctx, 
								transfer.FkAccountIDFrom, 
								transfer.FkAccountIDTo, 
								transfer.Type,
								transfer.Status,
								time.Now(),
								transfer.Currency,
								transfer.Amount).Scan(&id)
	if err != nil {
		childLogger.Error().Err(err).Msg("Exec statement")
		return nil, errors.New(err.Error())
	}

	res_transfer := core.Transfer{}
	res_transfer.ID = id
	res_transfer.TransferAt = time.Now()

	defer stmt.Close()
	return &res_transfer , nil
}
