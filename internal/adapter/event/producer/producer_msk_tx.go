package producer

import (
	"encoding/json"
	"context"
	"fmt"
//	"time"

	"github.com/rs/zerolog/log"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-worker-transfer/internal/core"
	"go.opentelemetry.io/otel"
)

var childLogger = log.With().Str("adpater", "event.producer").Logger()

type ProducerWorker struct{
	configurations  *core.KafkaConfig
	producer        *kafka.Producer
}

func NewProducerWorker(ctx context.Context, configurations *core.KafkaConfig) ( *ProducerWorker, error) {
	childLogger.Debug().Msg("NewProducerWorker")

	kafkaBrokerUrls := 	configurations.KafkaConfigurations.Brokers1 + "," + configurations.KafkaConfigurations.Brokers2 + "," + configurations.KafkaConfigurations.Brokers3

	config := &kafka.ConfigMap{	"bootstrap.servers":            kafkaBrokerUrls,
								"security.protocol":            configurations.KafkaConfigurations.Protocol, //"SASL_SSL",
								"sasl.mechanisms":              configurations.KafkaConfigurations.Mechanisms, //"SCRAM-SHA-256",
								"sasl.username":                configurations.KafkaConfigurations.Username,
								"sasl.password":                configurations.KafkaConfigurations.Password,
								"acks": 						"all", // acks=0  acks=1 acks=all
								"message.timeout.ms":			5000,
								"retries":						5,
								"retry.backoff.ms":				500,
								"enable.idempotence":			true,
								"go.logs.channel.enable": 		true, 
								"transactional.id":       		fmt.Sprintf("go-transactions-example-p%d", 0),                   
								}

	producer, err := kafka.NewProducer(config)
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to NewProducer")
		return nil, err
	}

	err = producer.InitTransactions(ctx);
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to InitTransactions")
		return nil, err
	}

	return &ProducerWorker{ 	configurations : configurations,
								producer : producer,
	}, nil
}

func (p *ProducerWorker) Producer(ctx context.Context, event core.Event) error{
	childLogger.Debug().Msg("Producer")

	ctx, svcspan := otel.Tracer("go-worker-transfer").Start(ctx,"event.producer")
	defer svcspan.End()

	payload, err := json.Marshal(event)
	if err != nil {
		childLogger.Error().Err(err).Msg("Erro no Marshall")
		return err
	}
	key	:= event.Key

	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")
	childLogger.Debug().Interface("Topic ==>",event.EventType).Msg("")
	childLogger.Debug().Interface("Key   ==>",key).Msg("")
	childLogger.Debug().Interface("Event ==>",event.EventData).Msg("")
	childLogger.Debug().Msg("++++++++++++++++++++++++++++++++")

	deliveryChan := make(chan kafka.Event)
	err = p.producer.Produce(	&kafka.Message	{	TopicPartition: kafka.TopicPartition{	Topic: &event.EventType, 
																						Partition: kafka.PartitionAny },
									Key:    []byte(key),											
									Value: 	[]byte(payload), 
									Headers:  []kafka.Header{{Key: "ACCOUNT",Value: []byte(key) }},
								},
							deliveryChan)
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to producer message")
		return err
	}
	e := <-deliveryChan
	m := e.(*kafka.Message)

	if m.TopicPartition.Error != nil {
		childLogger.Debug().Msg("+ ERROR + + ERROR + +  ERROR +")	
		childLogger.Error().Err(m.TopicPartition.Error).Msg("Delivery failed")
		childLogger.Debug().Msg("+ ERROR + + ERROR + +  ERROR +")		
	} else {
		childLogger.Debug().Msg("+ + + + + + + + + + + + + + + + + + + + + + + +")		
		childLogger.Debug().Msg("Delivered message to topic")
		childLogger.Debug().Interface("topic  : ",*m.TopicPartition.Topic).Msg("")
		childLogger.Debug().Interface("partition  : ", m.TopicPartition.Partition).Msg("")
		childLogger.Debug().Interface("offset : ",m.TopicPartition.Offset).Msg("")
		childLogger.Debug().Msg("+ + + + + + + + + + + + + + + + + + + + + + + +")		
	}
	close(deliveryChan)

	return nil
}

func (p *ProducerWorker) BeginTransaction() error{
	err := p.producer.BeginTransaction();
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to BeginTransaction")
		return err
	}
	return nil
}

func (p *ProducerWorker) CommitTransaction(ctx context.Context) error{
	err := p.producer.CommitTransaction(ctx);
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to CommitTransaction")
		return err
	}
	return nil
}

func (p *ProducerWorker) AbortTransaction(ctx context.Context) error{
	err := p.producer.AbortTransaction(ctx);
	if err != nil {
		childLogger.Error().Err(err).Msg("Failed to AbortTransaction")
		return err
	}
	return nil
}
