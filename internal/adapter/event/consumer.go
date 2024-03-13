package event

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"context"
	"encoding/json"
	//"strconv"

	"github.com/rs/zerolog/log"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/service"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

)

var childLogger = log.With().Str("adpater", "event").Logger()

type ConsumerWorker struct{
	configurations  *core.KafkaConfig
	consumer        *kafka.Consumer
	workerService	*service.WorkerService
	infoPod 		*core.InfoPod
}

func NewConsumerWorker(	ctx context.Context, 
						configurations *core.KafkaConfig,
						workerService	*service.WorkerService,
						infoPod *core.InfoPod) (*ConsumerWorker, error) {
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
							infoPod: infoPod,
	}, nil
}

func (c *ConsumerWorker) Consumer(	ctx context.Context, 
									wg *sync.WaitGroup, 
									topic string) {
	childLogger.Debug().Msg("Consumer")

	// ---------------------- OTEL ---------------
	childLogger.Info().Str("OTEL_EXPORTER_OTLP_ENDPOINT :", c.infoPod.OtelExportEndpoint).Msg("")

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure(), otlptracegrpc.WithEndpoint(c.infoPod.OtelExportEndpoint)	)
	if err != nil {
		childLogger.Error().Err(err).Msg("ERRO otlptracegrpc")
	}
	idg := xray.NewIDGenerator()

	tp := sdktrace.NewTracerProvider(	sdktrace.WithSampler(sdktrace.AlwaysSample()),
										sdktrace.WithBatcher(traceExporter),
										sdktrace.WithIDGenerator(idg),
									)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(xray.Propagator{})
	// ----------------------------------

	defer func() {
		err = tp.Shutdown(ctx)
		if err != nil{
			childLogger.Error().Err(err).Msg("Erro closing OTEL tracer !!!")
		}
		childLogger.Debug().Msg("Closing consumer waiting please !!!")
		c.consumer.Close()
		wg.Done()
	}()

	topics := []string{topic}
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	err = c.consumer.SubscribeTopics(topics, nil)
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to subscriber topic")
	}

	run := true
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

					newSegment := "go-worker-transfer"
					ctx, svcspan := otel.Tracer(newSegment).Start(ctx,"adapter.event.consumer")

					//newSegment := "go-worker-transfer:"+ event.EventData.Transfer.AccountIDTo + ":" + strconv.Itoa(event.EventData.Transfer.ID)
					//ctx, svcspan := otel.Tracer(newSegment).Start(ctx,"adapter.event.consumer")
					//svcspan.End()

					log.Print("----------------------------------")
					if e.Headers != nil {
						log.Printf("Headers: %v\n", e.Headers)
					}
					log.Print("Value : " ,string(e.Value))
					log.Print("-----------------------------------")
					
					event := core.Event{}
					json.Unmarshal(e.Value, &event)
					
					err = c.workerService.Transfer(ctx, *event.EventData.Transfer)
					if err != nil {
						childLogger.Error().Err(err).Msg("Erro no Consumer.Transfer")
						childLogger.Debug().Msg("CONSUMER ROLLBACK!!!!")
					} else {
						childLogger.Debug().Msg("CONSUMER COMMIT!!!!")
						c.consumer.Commit()
					}

					svcspan.End()
					
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