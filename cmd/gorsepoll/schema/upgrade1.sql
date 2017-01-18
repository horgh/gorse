-- changing columns from TIMESTAMP WITHOUT TIME ZONE
-- to TIMESTAMP WITH TIME ZONE.

-- first the rss_feed table.

-- NULL at first since empty.
ALTER TABLE rss_feed
ADD COLUMN last_update_time_new TIMESTAMP WITH TIME ZONE;

-- import the data.
UPDATE rss_feed
SET last_update_time_new = last_update_time AT TIME ZONE 'UTC';

-- set NOT NULL.
ALTER TABLE rss_feed
ALTER COLUMN last_update_time_new SET NOT NULL;

-- drop the old column
ALTER TABLE rss_feed
DROP COLUMN last_update_time;

-- rename the new column.
ALTER TABLE rss_feed
RENAME COLUMN last_update_time_new TO last_update_time;

-- then the rss_item table.

-- NULL at first since empty.
ALTER TABLE rss_item
ADD COLUMN publication_date_new TIMESTAMP WITH TIME ZONE;

-- import the data.
UPDATE rss_item
SET publication_date_new = publication_date AT TIME ZONE 'UTC';

-- set NOT NULL.
ALTER TABLE rss_item
ALTER COLUMN publication_date_new SET NOT NULL;

-- drop the old column
ALTER TABLE rss_item
DROP COLUMN publication_date;

-- rename the new column.
ALTER TABLE rss_item
RENAME COLUMN publication_date_new TO publication_date;
