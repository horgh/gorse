// Package gorse holds functions common to the different tools making up the
// project.
package gorse

import (
	"database/sql"
	"fmt"
	"time"
)

// ReadState holds an item's state (rss_item_state table, read_state type).
type ReadState int

const (
	// Unread means the item is not yet read.
	Unread ReadState = iota
	// Read means the item was read.
	Read
	// ReadLater means to save the item to read later.
	ReadLater
)

// DBItem represents an item in the database.
type DBItem struct {
	ID              int64
	Title           string
	Description     string
	Link            string
	RSSFeedID       int64
	PublicationDate time.Time
	GUID            *string
}

// DBSetItemReadState sets the item's read state for the user.
func DBSetItemReadState(db *sql.DB, id int64, userID int,
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
		return fmt.Errorf("unable to set read state on item: %d: %s", id, err)
	}

	return nil
}

// Turn read state into the enumerated type in the database (read_state).
func (s ReadState) String() string {
	if s == Unread {
		return "unread"
	}
	if s == Read {
		return "read"
	}
	return "read-later"
}

// FindItemByLink retrieves an item's information from the database by feed and
// link. Link is unique per feed.
func FindItemByLink(db *sql.DB, feedID int64, link string) (*DBItem, error) {
	query := `
SELECT
id, title, description, link, rss_feed_id, publication_date, guid
FROM rss_item
WHERE rss_feed_id = $1 AND
link = $2
`

	row := db.QueryRow(query, feedID, link)
	item := &DBItem{}
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&item.Description,
		&item.Link,
		&item.RSSFeedID,
		&item.PublicationDate,
		&item.GUID,
	); err != nil {
		return nil, fmt.Errorf("failed to scan row: %s", err)
	}

	return item, nil
}
