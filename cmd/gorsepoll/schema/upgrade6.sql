-- In this upgrade I add a GUID column to feed items.
--
-- This is a unique identifier that the feed may specify. It's optional though.

ALTER TABLE rss_item ADD COLUMN guid VARCHAR;
ALTER TABLE rss_item_archive ADD COLUMN guid VARCHAR;
