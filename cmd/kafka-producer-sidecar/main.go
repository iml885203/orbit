package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/iml885203/orbit/internal/kafkaproducer"
)

func main() {
	addr := getenv("HTTP_ADDR", ":8080")
	bootstrap := strings.Split(getenv("KAFKA_BOOTSTRAP_SERVERS", "orbit-kafka:29092"), ",")
	for i := range bootstrap {
		bootstrap[i] = strings.TrimSpace(bootstrap[i])
	}
	server := kafkaproducer.Server{BootstrapServers: bootstrap}
	log.Printf("kafka producer sidecar listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
