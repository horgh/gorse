-- Move items out from the archive into the main table.
--
-- For the migration to using GUIDs I find it will be useful to have all items
-- together.
--
-- I'm sort of rethinking having the archive table at all. It seems like it is
-- not truly archived if I need to keep looking at it in active code!

INSERT INTO rss_item
(id, title, description, link, rss_feed_id, publication_date, create_time,
  update_time, guid)
SELECT
id, title, description, link, rss_feed_id, publication_date, create_time,
  update_time, guid
FROM rss_item_archive
;

DELETE FROM rss_item_archive;
