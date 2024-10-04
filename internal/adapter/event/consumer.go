package event

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/service"
	"github.com/go-worker-transfer/internal/lib"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
)

var childLogger = log.With().Str("adpater", "event").Logger()
var tracer 			trace.Tracer

type ConsumerWorker struct{
	configurations  *core.KafkaConfig
	consumer        *kafka.Consumer
	workerService	*service.WorkerService
	configOTEL 		*core.ConfigOTEL
}

func NewConsumerWorker(	configurations *core.KafkaConfig,
						workerService	*service.WorkerService,
						configOTEL 		*core.ConfigOTEL) (*ConsumerWorker, error) {
	childLogger.Debug().Msg("NewConsumerWorker")

	kafkaBrokerUrls := 	configurations.KafkaConfigurations.Brokers1 + "," + configurations.KafkaConfigurations.Brokers2 + "," + configurations.KafkaConfigurations.Brokers3
	config := &kafka.ConfigMap{	"bootstrap.servers":            kafkaBrokerUrls,
								"security.protocol":            configurations.KafkaConfigurations.Protocol, //"SASL_SSL",
								"sasl.mechanisms":              configurations.KafkaConfigurations.Mechanisms, //"SCRAM-SHA-256",
								"sasl.username":                configurations.KafkaConfigurations.Username,
								"sasl.password":                configurations.KafkaConfigurations.Password,
								"group.id":                     configurations.KafkaConfigurations.Groupid,
								"enable.auto.commit":           false, //true,
								"broker.address.family": 		"v4",
								"client.id": 					configurations.KafkaConfigurations.Clientid,
								"session.timeout.ms":    		6000,
								"enable.idempotence":			true,
								// "auto.offset.reset":     		"latest", 
								"auto.offset.reset":     		"earliest",  
								}

	consumer, err := kafka.NewConsumer(config)
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to create consumer")
		return nil, err
	}

	return &ConsumerWorker{ configurations: configurations,
							consumer: 		consumer,
							workerService: 	workerService,
							configOTEL: 	configOTEL,
	}, nil
}

func (c *ConsumerWorker) Consumer(	ctx context.Context, 
									wg *sync.WaitGroup, 
									appServer core.WorkerAppServer) {
	childLogger.Debug().Msg("Consumer")

	// ---------------------- OTEL ---------------
	childLogger.Info().Str("OTEL_EXPORTER_OTLP_ENDPOINT :", c.configOTEL.OtelExportEndpoint).Msg("")

	tp := lib.NewTracerProvider(ctx, appServer.ConfigOTEL, appServer.InfoPod)
	otel.SetTextMapPropagator(xray.Propagator{})
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer(appServer.InfoPod.PodName)
	// ----------------------------------

	defer func() {
		err := tp.Shutdown(ctx)
		if err != nil{
			childLogger.Error().Err(err).Msg("Erro closing OTEL tracer !!!")
		}
		childLogger.Debug().Msg("Closing consumer waiting please !!!")
		c.consumer.Close()
		wg.Done()
	}()

	topics := []string{appServer.KafkaConfig.Topic.Transfer}
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	err := c.consumer.SubscribeTopics(topics, nil)
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to subscriber topic")
	}

	run := true
	event := core.Event{}
	for run {
		select {
			case sig := <-sigchan:
				childLogger.Debug().Interface("Caught signal terminating: ", sig).Msg("")
				run = false
			default:
				ev := c.consumer.Poll(100)
				if ev == nil {
					continue
				}
			switch e := ev.(type) {
				case kafka.AssignedPartitions:
					c.consumer.Assign(e.Partitions)
				case kafka.RevokedPartitions:
					c.consumer.Unassign()	
				case kafka.PartitionEOF:
					childLogger.Error().Interface("kafka.PartitionEOF: ",e).Msg("")
				case *kafka.Message:
					log.Print("----------------------------------")
					if e.Headers != nil {
						log.Printf("Headers: %v\n", e.Headers)
					}
					log.Print("Value : " ,string(e.Value))
					log.Print("-----------------------------------")
					
					json.Unmarshal(e.Value, &event)

					ctx, span := tracer.Start(ctx, "go-worker-transfer:" + event.EventData.Transfer.AccountIDFrom + ":" + event.EventData.Transfer.AccountIDTo)
			
					err = c.workerService.Transfer(ctx, *event.EventData.Transfer)
					if err != nil {
						childLogger.Error().Err(err).Msg("Erro no Consumer.Transfer")
						childLogger.Debug().Msg("CONSUMER ROLLBACK!!!!")
					} else {
						childLogger.Debug().Msg("CONSUMER COMMIT!!!!")
						c.consumer.Commit()
					}

					span.End()
				case kafka.Error:
					childLogger.Error().Err(e).Msg("kafka.Error")
					if e.Code() == kafka.ErrAllBrokersDown {
						run = false
					}
				default:
					childLogger.Debug().Interface("default: ",e).Msg("Ignored")
			}
		}
	}

}