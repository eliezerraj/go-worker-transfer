package service

import (
	"context"
	"time"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/erro"
	"go.opentelemetry.io/otel"

)

func (s WorkerService) Transfer(ctx context.Context, transfer core.Transfer) (error){
	childLogger.Debug().Msg("Transfer")
	childLogger.Debug().Interface("===>transfer:",transfer).Msg("")
	
	ctx, svcspan := otel.Tracer("go-worker-transfer").Start(ctx,"svc.Transfer")
	defer svcspan.End()

	tx, err := s.workerRepository.StartTx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	// Debit
	// Register the moviment into table transfer_moviment (work as a history)
	transferFrom := transfer
	transferFrom.Status = "DEBIT_EVENT_CREATED"
	transferFrom.FkAccountIDTo = transferFrom.FkAccountIDFrom
	transferFrom.AccountIDTo = transferFrom.AccountIDFrom
	transferFrom.Type = "DEBIT"
	transferFrom.Amount = (transferFrom.Amount * -1)

	res, err := s.workerRepository.AddTransferMoviment(ctx, tx, transferFrom)
	if err != nil {
		return err
	}

	transferFrom.ID = res.ID
	eventDataFrom := core.EventData{&transferFrom}
	event := core.Event{
		Key: transfer.AccountIDFrom,
		EventDate: time.Now(),
		EventType: s.topic.Dedit,
		EventData:	&eventDataFrom,	
	}

	childLogger.Debug().Interface("===>eventDataFrom:",eventDataFrom).Msg("")

	err = s.producerWorker.Producer(ctx, event)
	if err != nil {
		return err
	}

	transferFrom.Status = "DEBIT_SCHEDULE"
	res_update, err := s.workerRepository.Update(ctx,tx ,transferFrom)
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

	res, err = s.workerRepository.AddTransferMoviment(ctx, tx, transferTo)
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

	childLogger.Debug().Interface("===>eventDataTo:",eventDataTo).Msg("")

	err = s.producerWorker.Producer(ctx, event)
	if err != nil {
		return err
	}

	transferTo.Status = "CREDIT_SCHEDULE"
	res_update, err = s.workerRepository.Update(ctx,tx ,transferTo)
	if err != nil {
		return err
	}
	if res_update == 0 {
		err = erro.ErrUpdate
		return err
	}

	// ----------------------------------
	transfer.Status = "TRANSFER_DONE"
	res_update, err = s.workerRepository.Update(ctx,tx ,transfer)
	if err != nil {
		return err
	}
	if res_update == 0 {
		err = erro.ErrUpdate
		return err
	}

	if transfer.Status != "TRANSFER_DONE"{
		return erro.ErrEvent
	}

	return nil
}
