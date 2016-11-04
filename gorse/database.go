package main

import (
	"database/sql"
	"fmt"
	"log"

	"summercat.com/gorse/gorselib"
)

// Retrieve an item's information from the database. This includes the item's
// state for the given user.
func dbGetItem(db *sql.DB, itemID int64, userID int) (gorselib.RSSItem, error) {
	query := `
SELECT
ri.id, ri.title, ri.description, ri.link, ri.publication_date,
ri.rss_feed_id, rf.name, COALESCE(ris.state, 'unread')
FROM rss_item ri
JOIN rss_feed rf ON ri.rss_feed_id = rf.id
LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
WHERE ri.id = $1 AND
COALESCE(ris.user_id, $2) = $3
`
	rows, err := db.Query(query, itemID, userID, userID)
	if err != nil {
		return gorselib.RSSItem{}, err
	}

	for rows.Next() {
		item := gorselib.RSSItem{}

		err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.URI,
			&item.PublicationDate, &item.FeedID, &item.FeedName, &item.ReadState)
		if err != nil {
			_ = rows.Close()
			return gorselib.RSSItem{}, fmt.Errorf("Failed to scan row: %s", err)
		}

		_ = rows.Close()
		return item, nil
	}

	err = rows.Err()
	if err != nil {
		return gorselib.RSSItem{}, fmt.Errorf("Failure fetching rows: %s", err)
	}

	return gorselib.RSSItem{}, fmt.Errorf("Item not found.")
}

// dbSetItemReadState sets the given item's state in the database.
func dbSetItemReadState(db *sql.DB, id int64, userID int,
	state ReadState) error {
	// Upsert.
	query := `
INSERT INTO rss_item_state
(user_id, item_id, state)
VALUES($1, $2, $3)
ON CONFLICT (user_id, item_id) DO UPDATE
SET state = $4
`
	_, err := db.Exec(query, userID, id, state.String(), state.String())
	if err != nil {
		log.Printf("Failed to set item id [%d] read: %s", id, err)
		return err
	}

	log.Printf("Set item id [%d] %s", id, state)
	return nil
}

// Record the item was read after having been archived.
//
// It is useful to be able to refer back to such items as it is likely they were
// looked at more closely than others.
func dbRecordReadAfterArchive(db *sql.DB, userID int,
	item gorselib.RSSItem) error {
	query := `
INSERT INTO rss_item_read_after_archive
(user_id, rss_feed_id, rss_item_id)
VALUES($1, $2, $3)
`
	_, err := db.Exec(query, userID, item.FeedID, item.ID)
	if err != nil {
		return fmt.Errorf("Unable to insert: %s", err)
	}

	log.Printf("Recorded item id %d as read after archive.", item.ID)
	return nil
}
