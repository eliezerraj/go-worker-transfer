package pg

import (
	"context"
	"fmt"
	"time"
	"errors"

	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/lib"
	"github.com/go-worker-transfer/internal/erro"

	"github.com/rs/zerolog/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var childLogger = log.With().Str("repository.pg", "WorkerRepo").Logger()

type DatabasePG interface {
	GetConnection() (*pgxpool.Pool)
	Acquire(context.Context) (*pgxpool.Conn, error)
	Release(*pgxpool.Conn)
}

type DatabasePGServer struct {
	connPool   	*pgxpool.Pool
}

func Config(database_url string) (*pgxpool.Config) {
	const defaultMaxConns = int32(10)
	const defaultMinConns = int32(3)
	const defaultMaxConnLifetime = time.Hour
	const defaultMaxConnIdleTime = time.Minute * 30
	const defaultHealthCheckPeriod = time.Minute
	const defaultConnectTimeout = time.Second * 5
   
	dbConfig, err := pgxpool.ParseConfig(database_url)
	if err!=nil {
		childLogger.Error().Err(err).Msg("Failed to create a config")
	}
   
	dbConfig.MaxConns = defaultMaxConns
	dbConfig.MinConns = defaultMinConns
	dbConfig.MaxConnLifetime = defaultMaxConnLifetime
	dbConfig.MaxConnIdleTime = defaultMaxConnIdleTime
	dbConfig.HealthCheckPeriod = defaultHealthCheckPeriod
	dbConfig.ConnConfig.ConnectTimeout = defaultConnectTimeout
   
	dbConfig.BeforeAcquire = func(ctx context.Context, c *pgx.Conn) bool {
		childLogger.Debug().Msg("Before acquiring connection pool !")
	 	return true
	}
   
	dbConfig.AfterRelease = func(c *pgx.Conn) bool {
		childLogger.Debug().Msg("After releasing connection pool !")
	 	return true
	}
   
	dbConfig.BeforeClose = func(c *pgx.Conn) {
		childLogger.Debug().Msg("Closed connection pool !")
	}
   
	return dbConfig
}

func NewDatabasePGServer(ctx context.Context, databaseRDS *core.DatabaseRDS) (DatabasePG, error) {
	childLogger.Debug().Msg("NewDatabasePGServer")
	
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", 
							databaseRDS.User, 
							databaseRDS.Password, 
							databaseRDS.Host, 
							databaseRDS.Port, 
							databaseRDS.DatabaseName) 
							
	connPool, err := pgxpool.NewWithConfig(ctx, Config(connStr))
	if err != nil {
		return DatabasePGServer{}, err
	}
	
	err = connPool.Ping(ctx)
	if err != nil {
		return DatabasePGServer{}, err
	}

	return DatabasePGServer{
		connPool: connPool,
	}, nil
}

func (d DatabasePGServer) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	childLogger.Debug().Msg("Acquire")

	connection, err := d.connPool.Acquire(ctx)
	if err != nil {
		childLogger.Error().Err(err).Msg("Error while acquiring connection from the database pool!!")
		return nil, err
	} 
	return connection, nil
}

func (d DatabasePGServer) Release(connection *pgxpool.Conn) {
	childLogger.Debug().Msg("Release")
	defer connection.Release()
}

func (d DatabasePGServer) GetConnection() (*pgxpool.Pool) {
	childLogger.Debug().Msg("GetConnection")
	return d.connPool
}

func (d DatabasePGServer) CloseConnection() {
	childLogger.Debug().Msg("CloseConnection")
	defer d.connPool.Close()
}

func (d DatabasePGServer) Ping(ctx context.Context) error {
	return d.connPool.Ping(ctx)
}
//-----------------------------------------------
type WorkerRepository struct {
	databasePG DatabasePG
}

func NewWorkerRepository(databasePG DatabasePG) WorkerRepository {
	childLogger.Debug().Msg("NewWorkerRepository")
	return WorkerRepository{
		databasePG: databasePG,
	}
}

func (w WorkerRepository) SetSessionVariable(ctx context.Context, userCredential string) (bool, error) {
	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")
	childLogger.Debug().Msg("SetSessionVariable")

	conn, err := w.databasePG.Acquire(ctx)
	if err != nil {
		childLogger.Error().Err(err).Msg("Erro Acquire")
		return false, errors.New(err.Error())
	}
	defer w.databasePG.Release(conn)
	
	_, err = conn.Query(ctx, "SET sess.user_credential to '" + userCredential+ "'")
	if err != nil {
		childLogger.Error().Err(err).Msg("SET SESSION statement ERROR")
		return false, errors.New(err.Error())
	}

	return true, nil
}

func (w WorkerRepository) GetSessionVariable(ctx context.Context) (*string, error) {
	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")
	childLogger.Debug().Msg("GetSessionVariable")

	conn, err := w.databasePG.Acquire(ctx)
	if err != nil {
		childLogger.Error().Err(err).Msg("Erro Acquire")
		return nil, errors.New(err.Error())
	}
	defer w.databasePG.Release(conn)

	var res_balance string
	rows, err := conn.Query(ctx, "SELECT current_setting('sess.user_credential')" )
	if err != nil {
		childLogger.Error().Err(err).Msg("Prepare statement")
		return nil, errors.New(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		err := rows.Scan( &res_balance )
		if err != nil {
			childLogger.Error().Err(err).Msg("Scan statement")
			return nil, errors.New(err.Error())
        }
		return &res_balance, nil
	}

	return nil, erro.ErrNotFound
}

func (w WorkerRepository) StartTx(ctx context.Context) (pgx.Tx, *pgxpool.Conn,error) {
	childLogger.Debug().Msg("StartTx")

	span := lib.Span(ctx, "repo.StartTx")
	defer span.End()

	span = lib.Span(ctx, "repo.Acquire")
	conn, err := w.databasePG.Acquire(ctx)
	if err != nil {
		childLogger.Error().Err(err).Msg("Erro Acquire")
		return nil, nil, errors.New(err.Error())
	}
	span.End()
	
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
//----------------------------
func (w WorkerRepository) Update(ctx context.Context, tx pgx.Tx, transfer core.Transfer) (int64, error){
	childLogger.Debug().Msg("Update")
	childLogger.Debug().Interface("transfer : ", transfer).Msg("")

	span := lib.Span(ctx, "repo.Update")	
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

func (w WorkerRepository) AddTransferMoviment(ctx context.Context, tx pgx.Tx ,transfer core.Transfer) (*core.Transfer, error){
	childLogger.Debug().Msg("AddTransferMoviment")
	childLogger.Debug().Interface("transfer:",transfer).Msg("")

	span := lib.Span(ctx, "repo.AddTransferMoviment")	
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

	return &transfer , nil
}