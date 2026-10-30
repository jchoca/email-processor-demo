CREATE TABLE IF NOT EXISTS message_accepted
(
    eventId   UUID,
    messageId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);

CREATE TABLE IF NOT EXISTS message_processed
(
    eventId   UUID,
    messageId UUID,
    timestamp DateTime,
    tags      Array(String)
)
ENGINE = MergeTree
ORDER BY (timestamp, eventId);
