-- Set all read by a date cutoff.

-- Show which will be updated.
--SELECT
--ri.publication_date, rf.name, ri.title
--FROM rss_item ri
--LEFT JOIN rss_feed rf
--ON rf.id = ri.rss_feed_id
--WHERE ri.id NOT IN (SELECT item_id FROM rss_item_state) AND
--ri.publication_date < '2017-05-21'
--ORDER BY ri.publication_date
--;

-- Update them to 'read'.
INSERT INTO rss_item_state
(state, item_id, user_id)
SELECT
'read', ri.id, 1
FROM rss_item ri
WHERE ri.id NOT IN (SELECT item_id FROM rss_item_state) AND
ri.publication_date < '2017-05-21'
;

-- Set all read by a feed.
INSERT INTO rss_item_state
(state, item_id, user_id)
SELECT
'read', ri.id, 1
FROM rss_item ri
WHERE ri.id NOT IN (SELECT item_id FROM rss_item_state) AND
ri.rss_feed_id = 53
;
