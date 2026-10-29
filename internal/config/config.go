// Package config reads shared settings from the environment.
package config

import (
	"os"
	"strings"
)

// TopicAccepted is the topic the API produces to and the worker consumes.
const TopicAccepted = "message-accepted"

// Brokers returns the Kafka brokers from KAFKA_BROKERS (comma separated).
func Brokers() []string {
	if b := os.Getenv("KAFKA_BROKERS"); b != "" {
		return strings.Split(b, ",")
	}
	return []string{"localhost:9092"}
}

// Port returns the HTTP port from PORT.
func Port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
