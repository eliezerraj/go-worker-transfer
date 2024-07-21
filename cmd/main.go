package main

import(
	"sync"
	"context"
	"time"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/go-worker-transfer/internal/util"
	"github.com/go-worker-transfer/internal/adapter/event"
	"github.com/go-worker-transfer/internal/adapter/event/producer"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/service"
	"github.com/go-worker-transfer/internal/repository/pg"
)

var(
	logLevel 	= 	zerolog.DebugLevel
	appServer	core.WorkerAppServer
)

func init() {
	log.Debug().Msg("init")
	zerolog.SetGlobalLevel(logLevel)

	infoPod, restEndpoint := util.GetInfoPod()
	database := util.GetDatabaseEnv()
	configOTEL := util.GetOtelEnv()
	kafkaConfig := util.GetKafkaEnv()

	appServer.InfoPod = &infoPod
	appServer.Database = &database
	appServer.RestEndpoint = &restEndpoint
	appServer.ConfigOTEL = &configOTEL
	appServer.KafkaConfig = &kafkaConfig
}

func main()  {
	log.Debug().Msg("----------------------------------------------------")
	log.Debug().Msg("main")
	log.Debug().Msg("----------------------------------------------------")
	log.Debug().Interface("appServer :",appServer).Msg("")
	log.Debug().Msg("----------------------------------------------------")

	ctx := context.Background()

	// Open Database
	count := 1
	var databasePG	pg.DatabasePG
	var err error
	for {
		databasePG, err = pg.NewDatabasePGServer(ctx, appServer.Database)
		if err != nil {
			if count < 3 {
				log.Error().Err(err).Msg("Erro open Database... trying again !!")
			} else {
				log.Error().Err(err).Msg("Fatal erro open Database aborting")
				panic(err)
			}
			time.Sleep(3 * time.Second)
			count = count + 1
			continue
		}
		break
	}

	repoDB := pg.NewWorkerRepository(databasePG)
	producerWorker, err := producer.NewProducerWorker(	ctx, 
														appServer.KafkaConfig)
	if err != nil {
		log.Error().Err(err).Msg("Erro na criacao do producer Kafka")
	}

	workerService := service.NewWorkerService(	&repoDB, 
												producerWorker,
												appServer.KafkaConfig.Topic)

	consumerWorker, err := event.NewConsumerWorker(	ctx, 
													appServer.KafkaConfig, 
													workerService,
													appServer.ConfigOTEL)
	if err != nil {
		log.Error().Err(err).Msg("Erro na abertura do Kafka")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go consumerWorker.Consumer(	ctx, 
								&wg, 
								appServer)
	wg.Wait()
}