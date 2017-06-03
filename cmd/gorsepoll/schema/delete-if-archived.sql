-- Delete items from the main table if they are in the archive table.
--
-- This is useful if you make a mistake and re-add items!

DELETE FROM rss_item ri
WHERE
ri.link IN (
  SELECT ria.link
  FROM rss_item_archive ria
)
;
