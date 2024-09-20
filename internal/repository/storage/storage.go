package storage

import (
	"context"
	"errors"
	"time"

	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/lib"
	"github.com/go-worker-transfer/internal/repository/pg"

	"github.com/rs/zerolog/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var childLogger = log.With().Str("repository", "storage").Logger()

type WorkerRepository struct {
	databasePG pg.DatabasePG
}

func NewWorkerRepository(databasePG pg.DatabasePG) WorkerRepository {
	childLogger.Debug().Msg("NewWorkerRepository")
	return WorkerRepository{
		databasePG: databasePG,
	}
}

func (w WorkerRepository) StartTx(ctx context.Context) (pgx.Tx, *pgxpool.Conn,error) {
	childLogger.Debug().Msg("StartTx")

	span := lib.Span(ctx, "storage.StartTx")
	defer span.End()

	conn, err := w.databasePG.Acquire(ctx)
	if err != nil {
		childLogger.Error().Err(err).Msg("Erro Acquire")
		return nil, nil, errors.New(err.Error())
	}
	
	tx, err := conn.Begin(ctx)
    if err != nil {
        return nil, nil ,errors.New(err.Error())
    }

	return tx, conn, nil
}

func (w WorkerRepository) ReleaseTx(connection *pgxpool.Conn) {
	childLogger.Debug().Msg("ReleaseTx")

	defer connection.Release()
}

func (w WorkerRepository) Update(ctx context.Context, tx pgx.Tx, transfer *core.Transfer) (int64, error){
	childLogger.Debug().Msg("Update")
	childLogger.Debug().Interface("transfer : ", transfer).Msg("")

	span := lib.Span(ctx, "storage.Update")	
    defer span.End()

	query := `Update transfer_moviment
				set status = $2
				where id = $1 `

	row, err := tx.Exec(ctx, query, transfer.ID, transfer.Status)
	if err != nil {
		childLogger.Error().Err(err).Msg("Exec statement")
		return 0, errors.New(err.Error())
	}

	childLogger.Debug().Interface("rowsAffected : ", row.RowsAffected()).Msg("")

	return int64(row.RowsAffected()) , nil
}

func (w WorkerRepository) AddTransferMoviment(ctx context.Context, tx pgx.Tx ,transfer *core.Transfer) (*core.Transfer, error){
	childLogger.Debug().Msg("AddTransferMoviment")
	childLogger.Debug().Interface("transfer:",transfer).Msg("")

	span := lib.Span(ctx, "storage.AddTransferMoviment")	
    defer span.End()

	transfer.TransferAt = time.Now()

	query := `INSERT INTO transfer_moviment(fk_account_id_from, 
											fk_account_id_to,
											type_charge,
											status,  
											transfer_at,
											currency,
											amount) 
				VALUES($1, $2, $3, $4, $5, $6, $7) RETURNING id`

	row := tx.QueryRow(ctx, query, transfer.FkAccountIDFrom, 
									transfer.FkAccountIDTo, 
									transfer.Type,
									transfer.Status,
									transfer.TransferAt,
									transfer.Currency,
									transfer.Amount)								
																
	var id int
	if err := row.Scan(&id); err != nil {
		childLogger.Error().Err(err).Msg("INSERT statement")
		return nil, errors.New(err.Error())
	}

	transfer.ID = id

	return transfer , nil
}