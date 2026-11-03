CREATE DATABASE IF NOT EXISTS email_processor_demo;

CREATE TABLE IF NOT EXISTS email_processor_demo.message_accepted
(
    eventId   UUID,
    messageId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);

CREATE TABLE IF NOT EXISTS email_processor_demo.message_processed
(
    eventId   UUID,
    messageId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);
