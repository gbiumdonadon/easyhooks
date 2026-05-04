package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/easyhooks/easyhooks/internal/config"
)

// NewProducer creates a franz-go Kafka producer with settings matching Python's aiokafka config.
// linger=5ms, gzip compression, acks=leader (1), max batch=32KB.
func NewProducer(cfg *config.Config) (*kgo.Client, error) {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(cfg.KafkaWebhookTopic),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.ProducerBatchMaxBytes(32*1024),
		kgo.RequiredAcks(kgo.LeaderAck()),
		kgo.RecordPartitioner(kgo.RoundRobinPartitioner()),
	)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}
	return client, nil
}

// NewDLQProducer creates a producer dedicated to the DLQ topic.
func NewDLQProducer(cfg *config.Config) (*kgo.Client, error) {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.RequiredAcks(kgo.LeaderAck()),
	)
	if err != nil {
		return nil, fmt.Errorf("create dlq producer: %w", err)
	}
	return client, nil
}

// ProduceWebhookMessage sends a webhook event to the inbound Kafka topic.
// Headers: tenant_id, event_id — matches Python's kafka_producer.py.
func ProduceWebhookMessage(ctx context.Context, client *kgo.Client, topic, tenantID, eventID string, payload []byte) error {
	record := &kgo.Record{
		Topic: topic,
		Value: payload,
		Headers: []kgo.RecordHeader{
			{Key: "tenant_id", Value: []byte(tenantID)},
			{Key: "event_id", Value: []byte(eventID)},
		},
	}
	results := client.ProduceSync(ctx, record)
	return results.FirstErr()
}
