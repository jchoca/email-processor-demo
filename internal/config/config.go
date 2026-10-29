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

// SMTPAddr returns the SMTP server from SMTP_ADDR.
func SMTPAddr() string {
	if a := os.Getenv("SMTP_ADDR"); a != "" {
		return a
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

// SenderDomains returns the allowed from-domains from ALLOWED_SENDER_DOMAINS.
// Empty means any sender is accepted (local demo default).
func SenderDomains() []string {
	var domains []string
	for _, d := range strings.Split(os.Getenv("ALLOWED_SENDER_DOMAINS"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}
