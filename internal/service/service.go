package service

import (
	"github.com/rs/zerolog/log"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/repository/postgre"
	"github.com/go-worker-transfer/internal/adapter/restapi"
	"github.com/go-worker-transfer/internal/adapter/event/producer"
)

var childLogger = log.With().Str("service", "service").Logger()

type WorkerService struct {
	workerRepository 		*db_postgre.WorkerRepository
	restapi					*restapi.RestApiSConfig
	producerWorker			*producer.ProducerWorker
	topic					*core.Topic
}

func NewWorkerService(	workerRepository *db_postgre.WorkerRepository,
						restapi	*restapi.RestApiSConfig,
						producerWorker		*producer.ProducerWorker,
						topic				*core.Topic ) *WorkerService{
	childLogger.Debug().Msg("NewWorkerService")

	return &WorkerService{
		workerRepository:	workerRepository,
		restapi:			restapi,
		producerWorker: 	producerWorker,
		topic:				topic,
	}
}
