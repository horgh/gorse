-- In this upgrade we add a column to track the latest raw payload/feed for each
-- feed. We store one per feed.

ALTER TABLE rss_feed
ADD COLUMN last_payload bytea;
