package event

import (
	"os"
	"os/signal"
	"syscall"
	"sync"
	"context"
	"encoding/json"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-worker-transfer/internal/core"
	"github.com/go-worker-transfer/internal/service"
	"go.opentelemetry.io/otel"
)

var childLogger = log.With().Str("adpater", "event").Logger()

type ConsumerWorker struct{
	configurations  *core.KafkaConfig
	consumer        *kafka.Consumer
	workerService	*service.WorkerService
}

func NewConsumerWorker(	ctx context.Context, 
						configurations *core.KafkaConfig,
						workerService	*service.WorkerService ) (*ConsumerWorker, error) {
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
	}, nil
}

func (c *ConsumerWorker) Consumer(ctx context.Context, wg *sync.WaitGroup, topic string) {
	childLogger.Debug().Msg("Consumer")

	topics := []string{topic}
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	err := c.consumer.SubscribeTopics(topics, nil)
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
					
					log.Print("----------------------------------")
					if e.Headers != nil {
						log.Printf("Headers: %v\n", e.Headers)
					}
					log.Print("Value : " ,string(e.Value))
					log.Print("-----------------------------------")
					
					event := core.Event{}
					json.Unmarshal(e.Value, &event)

					newSegment := "go-worker-transfer:"+ event.EventData.Transfer.AccountIDTo + ":" + strconv.Itoa(event.EventData.Transfer.ID)
					ctx, svcspan := otel.Tracer(newSegment).Start(ctx,"svc.Transfer")
					defer svcspan.End()

					err = c.workerService.Transfer(ctx, *event.EventData.Transfer)
					if err != nil {
						childLogger.Error().Err(err).Msg("Erro no Consumer.Transfer")
						childLogger.Debug().Msg("ROLLBACK!!!!")
					} else {
						childLogger.Debug().Msg("COMMIT!!!!")
						c.consumer.Commit()
					}
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

	childLogger.Debug().Msg("Closing consumer waiting please !!!")
	c.consumer.Close()
	defer wg.Done()
}