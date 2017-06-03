-- In this upgrade I add a GUID column to feed items.
--
-- This is a unique identifier that the feed may specify. It's optional though.
--
-- I also remove the NOT NULL constraint on the last_update_time column. I'm
-- going to use it as NULL to know that we haven't polled a feed yet.

ALTER TABLE rss_item ADD COLUMN guid VARCHAR;
ALTER TABLE rss_item_archive ADD COLUMN guid VARCHAR;

ALTER TABLE rss_feed ALTER COLUMN last_update_time DROP NOT NULL;
