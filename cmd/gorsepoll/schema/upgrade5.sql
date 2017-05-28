-- In this upgrade I add a column to a feed to set it to archive only.
--
-- This means any new items found get automatically set to read when they are
-- recorded.

ALTER TABLE rss_feed ADD COLUMN archive BOOLEAN;

UPDATE rss_feed SET archive = false;

ALTER TABLE rss_feed ALTER COLUMN archive SET NOT NULL;
