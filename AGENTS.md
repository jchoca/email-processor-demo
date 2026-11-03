# AGENTS.md

Guidance for coding agents working in this repo.

## What this is

An **educational / illustrative project** accompanying a blog post, not a production
system. The goal is a small, readable end-to-end example of an email pipeline:
HTTP API → Kafka → worker → SMTP. Optimize for clarity and teachability over
robustness, scale or operational completeness.

Do not "harden" it unprompted (auth, TLS, rate limiting, metrics, retries with
backoff, DLQs, migrations, elaborate abstractions). If a shortcut is wrong for
production, say so in one line and leave the code simple. Conversely, do not copy
these shortcuts into real systems.

## Architecture

```
client ──POST /v1/send-transactional──> cmd/api ──> kafka topic "message-accepted"
                                                          │
                       ├── cmd/worker ──> SMTP (Mailpit locally)
                       │        └────────> kafka topic "message-processed"
                       └── redpanda connect streams ──> clickhouse
                                                   └──> opensearch
```

- `cmd/api` stamps `messageId`, `eventId` and `timestamp` (UUIDv4s), validates the
  payload and produces the JSON to `message-accepted`.
- `cmd/worker` consumes it, renders MIME (`internal/mailmsg`), sends via SMTP and
  then republishes the payload to `message-processed` stamped as its own event:
  fresh `eventId`, send-time `timestamp`, same `messageId`.
- `connect/configs/streams/*.yaml` are redpanda connect pipelines that read the
  topics, drop the body (`text`/`html` never leave the pipeline) and fan out to
  clickhouse (`message_accepted` / `message_processed` tables) and opensearch
  (`message-accepted` / `message-processed` indices).
- `mailpit` (`http://localhost:8025`) is the local SMTP sink; API is on `:8080`.

## Layout

```
cmd/api/         HTTP server + handler + tests
cmd/worker/      Kafka consumer, SMTP sender, retry loop + tests
internal/email/  Transactional/Address types, NewMessage()/Stamp(), validation
internal/mailmsg/ MIME rendering (multipart/alternative, quoted-printable)
internal/sender/  SMTP client: dial timeout, optional required STARTTLS, optional AUTH
internal/config/  env config: PORT, KAFKA_BROKERS, SMTP_*, ALLOWED_SENDER_DOMAINS
connect/         redpanda connect stream configs
clickhouse/      init.sql, run once on first clickhouse start (email_processor_demo
                 database with message_accepted, message_processed tables)
opensearch/      template.json, index template for message-* (loaded by the
                 opensearch-bootstrap service)
docker-compose.yml  kafka, kafka-bootstrap (topics), api, worker, mailpit, connect,
                 opensearch, opensearch-bootstrap (index template), clickhouse
Dockerfile       one image, `--build-arg CMD=api|worker`
```

Module path is `example.com/email-processor`.

## Commands

```
go test ./...            # unit tests (no Kafka/SMTP needed)
go vet ./... && gofmt -l .
docker compose up -d --build kafka kafka-bootstrap api worker mailpit
make up / make down      # same, via compose; down removes orphans
make demo                # POST three sample messages to a running stack
```

Send a sample request:

```
curl -X POST localhost:8080/v1/send-transactional -d '{
  "accountId": "0b6f1c1e-6f43-4c9e-9a8e-2f1d3b7a5c10",
  "from": {"name": "Alice", "address": "alice@x.com"},
  "to":   {"name": "Bob",   "address": "bob@x.com"},
  "subject": "hello", "text": "hi", "html": "<p>hi</p>"
}'
```

Then check `http://localhost:8025` (Mailpit) or `docker compose logs worker`.

Both events land in clickhouse and opensearch (body excluded):

```
curl -s 'localhost:9200/message-*/_search?q=subject:hello'
curl -s 'localhost:9200/message-*/_search?q=tags:vip'
curl -s localhost:9200/_cat/indices/message-*?v
docker compose exec clickhouse clickhouse-client --database email_processor_demo --query 'SELECT * FROM message_accepted'
```

## Payload rules (the contract to keep consistent)

- `accountId`: required, a UUID in 8-4-4-4-12 hex form (any version, either case).
  It is not checked against any account store.
- `from`, `to`: required, one address each, must parse as a bare addr-spec
  (display names belong in `.name`). There is no cc/bcc; mail goes to `to` only.
- `name` and `address` ≤ 254 characters (runes), `subject` ≤ 256 characters.
- `tags` is optional; at most 5 entries.
- `messageId`, `eventId` and `timestamp` are set by the API on every request;
  values sent by a client are overwritten. `messageId` identifies the mail and is
  shared by its accepted and processed events; `eventId` is per event.
- At least one of `html` / `text` is required.
- Whole request body ≤ 1 MB (`http.MaxBytesReader`, 413 over the limit).
- `from` domain must be in `ALLOWED_SENDER_DOMAINS` (empty = any, the demo
  default); a domain also authorises its subdomains. Enforced by API and worker.
- Validation lives in `internal/email.Validate()`; the worker re-validates.
- Responses: `202` accepted, `400` validation/JSON, `413` too large, `500` if the
  Kafka produce fails.

## Known deliberate shortcuts

- No API auth, no rate limiting.
- SMTP: `SMTP_TLS=false` in compose because Mailpit offers no STARTTLS/AUTH. Left
  at its default (true), the sender refuses to send without STARTTLS; set
  `SMTP_USERNAME`/`SMTP_PASSWORD` for relays that require AUTH (e.g. SES).
- Transient SMTP errors (4xx, network) retry forever with exponential backoff
  (1s doubling to 1 min), blocking the partition; SMTP 5xx is logged and dropped as
  permanent, as are unparseable or disallowed payloads. No DLQ.
- Commits are explicit: the worker uses `FetchMessage` + `CommitMessages` because
  kafka-go's `ReadMessage` commits *before* the message is processed.
- Sender allowlist is a plain domain list, not SES identity verification.
- SMTP gives no message ID back and no bounce/complaint feedback.
- Kafka message key is unset; one partition, so ordering is trivially preserved.
- Join the two clickhouse tables on `messageId` (rows share it), not on
  `eventId`. If the republish to `message-processed` fails it is logged and the
  offset is still committed (the mail already went out), so a processed row can be
  missing.
- ClickHouse runs with demo credentials from compose: user and password
  `email-processor-demo`, database `email_processor_demo`. The database name uses
  underscores because the image's entrypoint runs `CREATE DATABASE IF NOT EXISTS
  $CLICKHOUSE_DB` unquoted, which fails on dashes. The image's own `users.d`
  config removes the passwordless `default` user, so unauthenticated requests get
  401. Any client (DataGrip, DSN) needs user, password and database explicitly.
- `clickhouse/init.sql` only runs while the data dir is empty, and docker's
  anonymous volume keeps that dir across recreates — schema edits need
  `docker compose down -v clickhouse` or a manual
  `clickhouse-client < clickhouse/init.sql`.- opensearch runs with security disabled, so `:9200` is open and unauthenticated.
  Document `_id` is the `eventId`, so a redelivered message overwrites its doc
  instead of duplicating it; the clickhouse tables will duplicate on redelivery.
- The body is dropped in the connect pipeline, not by a mapping template, so a
  consumer reading the Kafka topics directly still sees `text`/`html`.
- No Message-ID header (server adds one), no DKIM/SPF, no bounce handling.
- API returns 500 if Kafka is unreachable; no queue or retry.
- `connect` streams and the worker both read `message-accepted` in different
  consumer groups (intentional: the demo shows both).

## Working style

- Keep diffs small and readable; the blog explains this code.
- New non-trivial logic (branching, parsing, retry/commit behavior) gets a small
  test; glue code does not.
- Prefer stdlib; the only direct dependency is `github.com/segmentio/kafka-go`.
  Don't add dependencies for a few lines.
- Comment only the why that the code cannot show (e.g. why commits are explicit).
- Update this file if you change architecture, commands or the payload contract.
