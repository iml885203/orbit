package container

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
	"gopkg.in/yaml.v3"
)

// KafkaInternalBootstrap is the PLAINTEXT listener address inside the Kafka container.
const KafkaInternalBootstrap = "localhost:29092"

// KafkaTopicsFile represents the structure of a kafka-topics.yaml file.
type KafkaTopicsFile struct {
	Topics []KafkaTopic `yaml:"topics"`
}

type KafkaTopic struct {
	Name       string `yaml:"name"`
	Partitions int    `yaml:"partitions"`
	Replicas   int    `yaml:"replicas"`
}

// retryUntil runs fn every interval until it succeeds, the deadline elapses,
// or ctx is cancelled. Returns fn's last error on timeout. Replaces the old
// fixed pre-init sleeps: a probe that observes readiness is bounded and
// self-documenting where a sleep is a guess.
func retryUntil(ctx context.Context, deadline, interval time.Duration, fn func() error) error {
	timeout := time.After(deadline)
	var lastErr error
	for {
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return lastErr
		case <-time.After(interval):
		}
	}
}

// RunInit executes post-healthy initialization for a container.
func (m *Manager) RunInit(ctx context.Context, name string, cfg *config.Container) error {
	if cfg.Init == nil {
		return nil
	}

	switch cfg.Init.Type {
	case "kafka_topics":
		return m.initKafkaTopics(ctx, name, cfg)
	case "mongo_rs":
		return m.initMongoRS(ctx, name, cfg)
	default:
		return fmt.Errorf("unknown init type %q for %s", cfg.Init.Type, name)
	}
}

func (m *Manager) initKafkaTopics(ctx context.Context, name string, cfg *config.Container) error {
	if cfg.Init.TopicsFile == "" {
		return fmt.Errorf("kafka_topics init requires topics_file")
	}

	data, err := os.ReadFile(cfg.Init.TopicsFile)
	if err != nil {
		return fmt.Errorf("reading topics file: %w", err)
	}

	var tf KafkaTopicsFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return fmt.Errorf("parsing topics file: %w", err)
	}

	containerName := m.ContainerName(name)

	// Internal PLAINTEXT listener, stable regardless of host port mapping
	bootstrap := KafkaInternalBootstrap

	// The container health check covers the host listener; the internal
	// listener the topics are created against can lag it. Probe with a cheap
	// --list until the broker answers instead of guessing with a sleep.
	if err := retryUntil(ctx, 60*time.Second, 2*time.Second, func() error {
		probe := exec.CommandContext(ctx, "docker", "exec", containerName,
			"kafka-topics", "--list", "--bootstrap-server", bootstrap)
		if out, err := probe.CombinedOutput(); err != nil {
			return fmt.Errorf("broker not ready: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("kafka broker readiness: %w", err)
	}

	// Create all topics in a single docker exec call for efficiency
	var topicArgs []string
	for _, topic := range tf.Topics {
		partitions := topic.Partitions
		if partitions == 0 {
			partitions = 1
		}
		replicas := topic.Replicas
		if replicas == 0 {
			replicas = 1
		}
		topicArgs = append(topicArgs,
			fmt.Sprintf("kafka-topics --create --bootstrap-server %s --topic %s --partitions %d --replication-factor %d --if-not-exists",
				bootstrap, topic.Name, partitions, replicas))
	}

	slog.Info("creating Kafka topics", "component", "init", "count", len(tf.Topics), "container", containerName)
	script := strings.Join(topicArgs, " && ")
	cmd := exec.CommandContext(ctx, "docker", "exec", containerName, "bash", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kafka topic creation: %s", strings.TrimSpace(string(out)))
	}
	slog.Info("Kafka topics created successfully", "component", "init")
	return nil
}

func (m *Manager) initMongoRS(ctx context.Context, name string, cfg *config.Container) error {
	containerName := m.ContainerName(name)

	members := cfg.Init.RSMembers
	if len(members) == 0 {
		members = []string{"localhost:27017"}
	}

	// Build rs.initiate() command
	memberDocs := make([]string, 0, len(members))
	for i, m := range members {
		memberDocs = append(memberDocs, fmt.Sprintf("{_id: %d, host: %q}", i, m))
	}
	rsInitCmd := fmt.Sprintf("rs.initiate({_id: 'rs0', members: [%s]})", strings.Join(memberDocs, ", "))

	slog.Info("initializing MongoDB replica set", "component", "init", "name", name)
	// The TCP health check passes before mongod accepts commands, so retry
	// rs.initiate until it answers (bounded) rather than sleeping and hoping.
	// "already initialized" (server code AlreadyInitialized) is success: a
	// restarted container keeps its replica set.
	return retryUntil(ctx, 30*time.Second, time.Second, func() error {
		cmd := exec.CommandContext(ctx, "docker", "exec", containerName,
			"mongosh", "--eval", rsInitCmd,
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		output := string(out)
		if strings.Contains(output, "already initialized") || strings.Contains(output, "AlreadyInitialized") {
			return nil
		}
		return fmt.Errorf("mongo rs init: %s", strings.TrimSpace(output))
	})
}
