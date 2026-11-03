CREATE DATABASE IF NOT EXISTS email_processor_demo;

CREATE TABLE IF NOT EXISTS email_processor_demo.message_accepted
(
    eventId   UUID,
    messageId UUID,
    accountId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);

CREATE TABLE IF NOT EXISTS email_processor_demo.message_processed
(
    eventId   UUID,
    messageId UUID,
    accountId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);

CREATE TABLE IF NOT EXISTS email_processor_demo.sums_by_hour (
    timestamp DateTime,
    accountId UUID,
    tags Array(String),
    accepted UInt32,
    processed UInt32
) ENGINE = SummingMergeTree
ORDER BY (accountId, tags, timestamp);

CREATE MATERIALIZED VIEW IF NOT EXISTS email_processor_demo.accepted_by_hour_mv to email_processor_demo.sums_by_hour AS
SELECT
    toStartOfHour(timestamp) AS timestamp,
    accountId,
    tags,
    1 AS accepted
FROM email_processor_demo.message_accepted;

CREATE MATERIALIZED VIEW IF NOT EXISTS email_processor_demo.processed_by_hour_mv to email_processor_demo.sums_by_hour AS
SELECT
    toStartOfHour(timestamp) AS timestamp,
    accountId,
    tags,
    1 AS processed
FROM email_processor_demo.message_processed;