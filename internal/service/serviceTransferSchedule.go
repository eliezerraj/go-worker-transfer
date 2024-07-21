package service

import (
	"context"
	"time"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/erro"
	"github.com/go-worker-transfer/internal/lib"

	"github.com/rs/zerolog/log"
	"github.com/go-worker-transfer/internal/repository/pg"
	"github.com/go-worker-transfer/internal/adapter/event/producer"
)

var childLogger = log.With().Str("service", "service").Logger()

type WorkerService struct {
	workerRepo		*pg.WorkerRepository
	producerWorker	*producer.ProducerWorker
	topic			*core.Topic
}

func NewWorkerService(	workerRepo		*pg.WorkerRepository,
						producerWorker	*producer.ProducerWorker,
						topic			*core.Topic ) *WorkerService{
	childLogger.Debug().Msg("NewWorkerService")

	return &WorkerService{
		workerRepo:	workerRepo,
		producerWorker: 	producerWorker,
		topic:				topic,
	}
}

func (s WorkerService) Transfer(ctx context.Context, 
								transfer core.Transfer) (error){
	childLogger.Debug().Msg("Transfer")
	childLogger.Debug().Interface("===>transfer:",transfer).Msg("")
	
	span := lib.Span(ctx, "service.Transfer")	

	tx, conn, err := s.workerRepo.StartTx(ctx)
	if err != nil {
		return err
	}
	
	err = s.producerWorker.BeginTransaction()
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to Kafka BeginTransaction")
		return err
	}

	defer func() {
		if err != nil {
			childLogger.Error().Err(err).Msg("service.Transfer ROLLBACK")
			err := s.producerWorker.AbortTransaction(ctx)
			if err != nil {
				childLogger.Error().Err(err).Msg("Failed to Kafka AbortTransaction")
			}
			tx.Rollback(ctx)
		} else {
			childLogger.Error().Err(err).Msg("service.Transfer COMMIT")
			err = s.producerWorker.CommitTransaction(ctx)
			if err != nil {
				childLogger.Error().Err(err).Msg("Failed to Kafka CommitTransaction")
			}
			tx.Commit(ctx)
		}
		s.workerRepo.ReleaseTx(conn)
		span.End()
	}()

	// Debit
	// Register the moviment into table transfer_moviment (work as a history)
	transferFrom := transfer
	transferFrom.Status = "DEBIT_EVENT_CREATED"
	transferFrom.FkAccountIDTo = transferFrom.FkAccountIDFrom
	transferFrom.AccountIDTo = transferFrom.AccountIDFrom
	transferFrom.Type = "DEBIT"
	transferFrom.Amount = (transferFrom.Amount * -1)

	res, err := s.workerRepo.AddTransferMoviment(ctx, tx, transferFrom)
	if err != nil {
		return err
	}

	transferFrom.ID = res.ID
	eventDataFrom := core.EventData{&transferFrom}
	event := core.Event{
		Key: transfer.AccountIDFrom,
		EventDate: time.Now(),
		EventType: s.topic.Debit,
		EventData:	&eventDataFrom,	
	}

	err = s.producerWorker.Producer(ctx, event)
	if err != nil {
		return err
	}

	transferFrom.Status = "DEBIT_SCHEDULE"
	res_update, err := s.workerRepo.Update(ctx,tx ,transferFrom)
	if err != nil {
		return err
	}
	if res_update == 0 {
		err = erro.ErrUpdate
		return err
	}

	//Credit
	transferTo := transfer
	transferTo.Status = "CREDIT_EVENT_CREATED"
	transferTo.FkAccountIDFrom = transferTo.FkAccountIDTo
	transferTo.AccountIDFrom = transferTo.AccountIDTo
	transferTo.Type = "CREDIT"

	res, err = s.workerRepo.AddTransferMoviment(ctx, tx, transferTo)
	if err != nil {
		return err
	}

	transferTo.ID = res.ID
	eventDataTo := core.EventData{&transferTo}
	event = core.Event{
		Key: transfer.AccountIDTo,
		EventDate: time.Now(),
		EventType: s.topic.Credit,
		EventData:	&eventDataTo,	
	}

	err = s.producerWorker.Producer(ctx, event)
	if err != nil {
		return err
	}

	transferTo.Status = "CREDIT_SCHEDULE"
	res_update, err = s.workerRepo.Update(ctx,tx ,transferTo)
	if err != nil {
		return err
	}
	if res_update == 0 {
		err = erro.ErrUpdate
		return err
	}

	// ----------------------------------
	transfer.Status = "TRANSFER_DONE"
	res_update, err = s.workerRepo.Update(ctx,tx ,transfer)
	if err != nil {
		return err
	}
	if res_update == 0 {
		err = erro.ErrUpdate
		return err
	}

	if transfer.Status != "TRANSFER_DONE"{
		err = erro.ErrEvent
		return err
	}

	if transfer.Currency != "BRL"{
		err = erro.ErrCurrency
		return err
	}

	return nil
}