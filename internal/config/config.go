// Package config reads shared settings from the environment.
package config

import (
	"os"
	"strings"
)

// TopicAccepted is the topic the API produces to and the worker consumes.
const TopicAccepted = "message-accepted"

// TopicProcessed is the topic the worker produces to after a successful send.
const TopicProcessed = "message-processed"

// Brokers returns the Kafka brokers from KAFKA_BROKERS (comma separated).
func Brokers() []string {
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		return strings.Split(brokers, ",")
	}
	return []string{"localhost:9092"}
}

// Port returns the HTTP port from PORT.
func Port() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return "8080"
}

// SMTPAddr returns the SMTP server from SMTP_ADDR.
func SMTPAddr() string {
	if addr := os.Getenv("SMTP_ADDR"); addr != "" {
		return addr
	}
	return "localhost:1025"
}

// SMTPUsername returns the SMTP AUTH username from SMTP_USERNAME.
func SMTPUsername() string { return os.Getenv("SMTP_USERNAME") }

// SMTPPassword returns the SMTP AUTH password from SMTP_PASSWORD.
func SMTPPassword() string { return os.Getenv("SMTP_PASSWORD") }

// SMTPRequireTLS reports whether STARTTLS is mandatory; SMTP_TLS=false turns it off.
func SMTPRequireTLS() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS"))) {
	case "false", "0", "no":
		return false
	}
	return true
}

// ClickHouseURL returns the clickhouse HTTP interface from CLICKHOUSE_URL.
func ClickHouseURL() string {
	if clickhouseURL := os.Getenv("CLICKHOUSE_URL"); clickhouseURL != "" {
		return clickhouseURL
	}
	return "http://localhost:8123"
}

// ClickHouseUser returns the clickhouse user from CLICKHOUSE_USER.
func ClickHouseUser() string { return os.Getenv("CLICKHOUSE_USER") }

// ClickHousePassword returns the clickhouse password from CLICKHOUSE_PASSWORD.
func ClickHousePassword() string { return os.Getenv("CLICKHOUSE_PASSWORD") }

// ClickHouseDatabase returns the clickhouse database from CLICKHOUSE_DATABASE.
func ClickHouseDatabase() string {
	if database := os.Getenv("CLICKHOUSE_DATABASE"); database != "" {
		return database
	}
	return "email_processor_demo"
}

// OpenSearchURL returns the opensearch cluster from OPENSEARCH_URL.
func OpenSearchURL() string {
	if opensearchURL := os.Getenv("OPENSEARCH_URL"); opensearchURL != "" {
		return opensearchURL
	}
	return "http://localhost:9200"
}

// SenderDomains returns the allowed from-domains from ALLOWED_SENDER_DOMAINS.
// Empty means any sender is accepted (local demo default).
func SenderDomains() []string {
	var domains []string
	for _, domain := range strings.Split(os.Getenv("ALLOWED_SENDER_DOMAINS"), ",") {
		if domain = strings.TrimSpace(domain); domain != "" {
			domains = append(domains, domain)
		}
	}
	return domains
}
