package kafkaproducer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type Server struct {
	BootstrapServers []string
}

type produceRequest struct {
	Key       string            `json:"key"`
	Value     json.RawMessage   `json:"value"`
	Headers   map[string]string `json:"headers"`
	Partition *int              `json:"partition"`
}

type produceResponse struct {
	Topic     string `json:"topic"`
	Partition int    `json:"partition,omitempty"`
	Encoding  string `json:"encoding"`
	Bytes     int    `json:"bytes"`
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /topics/{topic}/messages", s.handleProduce)
	return mux
}

func (s Server) handleProduce(w http.ResponseWriter, r *http.Request) {
	topic := strings.TrimSpace(r.PathValue("topic"))
	if topic == "" {
		writeError(w, http.StatusBadRequest, "topic is required")
		return
	}
	var req produceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request json: %v", err))
		return
	}
	if len(req.Value) == 0 {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}
	value, err := EncodeBase64GzipJSON(req.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.produce(r.Context(), topic, req, value); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	partition := 0
	if req.Partition != nil {
		partition = *req.Partition
	}
	writeJSON(w, http.StatusAccepted, produceResponse{
		Topic:     topic,
		Partition: partition,
		Encoding:  "base64-gzip-json",
		Bytes:     len(value),
	})
}

func (s Server) produce(ctx context.Context, topic string, req produceRequest, value []byte) error {
	if len(s.BootstrapServers) == 0 {
		return fmt.Errorf("KAFKA_BOOTSTRAP_SERVERS is required")
	}
	balancer := kafka.Balancer(&kafka.Hash{})
	if req.Partition != nil {
		balancer = fixedPartitionBalancer{partition: *req.Partition}
	}
	writer := &kafka.Writer{
		Addr:         kafka.TCP(s.BootstrapServers...),
		Topic:        topic,
		Balancer:     balancer,
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
	defer func() { _ = writer.Close() }()
	headers := make([]kafka.Header, 0, len(req.Headers))
	for key, value := range req.Headers {
		headers = append(headers, kafka.Header{Key: key, Value: []byte(value)})
	}
	produceCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return writer.WriteMessages(produceCtx, kafka.Message{
		Key:     []byte(req.Key),
		Value:   value,
		Headers: headers,
	})
}

type fixedPartitionBalancer struct {
	partition int
}

func (b fixedPartitionBalancer) Balance(_ kafka.Message, partitions ...int) int {
	for _, partition := range partitions {
		if partition == b.partition {
			return partition
		}
	}
	return b.partition
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
