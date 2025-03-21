package database

import (
	"context"
	"errors"
	
	"github.com/go-worker-transfer/internal/core/model"

	go_core_observ "github.com/eliezerraj/go-core/observability"
	go_core_pg "github.com/eliezerraj/go-core/database/pg"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
)

var tracerProvider go_core_observ.TracerProvider
var childLogger = log.With().Str("adapter", "database").Logger()

type WorkerRepository struct {
	DatabasePGServer *go_core_pg.DatabasePGServer
}

func NewWorkerRepository(databasePGServer *go_core_pg.DatabasePGServer) *WorkerRepository{
	childLogger.Info().Msg("NewWorkerRepository")

	return &WorkerRepository{
		DatabasePGServer: databasePGServer,
	}
}

func (w WorkerRepository) GetTransactionUUID(ctx context.Context) (*string, error){
	childLogger.Info().Interface("trace-resquest-id", ctx.Value("trace-request-id")).Msg("GetTransactionUUID")
	
	// Trace
	span := tracerProvider.Span(ctx, "database.GetTransactionUUID")
	defer span.End()

	conn, err := w.DatabasePGServer.Acquire(ctx)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	defer w.DatabasePGServer.Release(conn)

	// Prepare
	var uuid string

	// Query and Execute
	query := `SELECT uuid_generate_v4()`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		err := rows.Scan(&uuid) 
		if err != nil {
			return nil, errors.New(err.Error())
        }
		return &uuid, nil
	}
	
	return &uuid, nil
}

func (w WorkerRepository) UpdateTransferMovimentTransfer(ctx context.Context, tx pgx.Tx, transfer *model.Transfer) (int64, error){
	childLogger.Info().Interface("trace-resquest-id", ctx.Value("trace-request-id")).Msg("UpdateTransferMovimentTransfer")

	// trace
	span := tracerProvider.Span(ctx, "database.UpdateTransferMovimentTransfer")
	defer span.End()

	// query and execute
	query := `Update transfer_moviment
				set status = $2
				where id = $1 `

	row, err := tx.Exec(ctx, query, transfer.ID, transfer.Status)
	if err != nil {
		childLogger.Error().Err(err).Msg("Exec statement")
		return 0, errors.New(err.Error())
	}

	childLogger.Info().Interface("trace-resquest-id", ctx.Value("trace-request-id")).Interface("rowsAffected : ", row.RowsAffected()).Msg("")

	return int64(row.RowsAffected()) , nil
}
