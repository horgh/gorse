// Package gorse holds functions common to the different tools making up the
// project.
package gorse

import (
	"database/sql"
	"fmt"
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

// DBSetItemReadState sets the given item's state in the database.
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
