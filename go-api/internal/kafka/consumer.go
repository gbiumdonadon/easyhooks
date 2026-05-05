package kafka

import (
	"fmt"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/easyhooks/easyhooks/internal/config"
)

// NewConsumer creates a franz-go Kafka consumer for the webhook worker.
// Consumer group with manual commit and earliest offset reset.
func NewConsumer(cfg *config.Config) (*kgo.Client, error) {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(cfg.KafkaConsumerGroup),
		kgo.ConsumeTopics(cfg.KafkaWebhookTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("create kafka consumer: %w", err)
	}
	return client, nil
}
