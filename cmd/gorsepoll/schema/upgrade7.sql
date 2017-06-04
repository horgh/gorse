-- In this upgrade I remove the archive table.
--
-- It became a mess because to check if I had an item I needed to look for it in
-- both tables. Likewise for the newest publication date. Probably other things
-- too. It's not really an archive if it requires constant interaction.

DROP TABLE rss_item_archive;
