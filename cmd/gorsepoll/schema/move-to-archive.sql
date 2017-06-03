-- Archive anything older than 1 month that is read.
--
-- This is to keep the table used for active interaction smaller and quicker.

--INSERT INTO rss_item_archive
--(id, title, description, link, publication_date, rss_feed_id, create_time,
--  update_time, guid)
--SELECT ri.id, ri.title, ri.description, ri.link, ri.publication_date,
--  ri.rss_feed_id, ri.create_time, ri.update_time, ri.guid
--FROM rss_item ri
--LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
--WHERE ri.publication_date < NOW() - '1 months'::INTERVAL AND
--COALESCE(ris.state, 'unread') = 'read'
--;

DELETE FROM rss_item ri
WHERE
ri.id IN (
  SELECT ris.item_id
  FROM rss_item_state ris
  WHERE ris.item_id = ri.id AND
  ris.state = 'read'
) AND
ri.publication_date < NOW() - '1 months'::INTERVAL
;
