package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/horgh/gorse"
)

// DBItem holds the information about an input/entry that is in the database.
// TODO(will@summercat.com): Refactor to combine with gorse.DBItem. I think we
// should have one that is the generic item. This one includes a field related
// to a single user.
type DBItem struct {
	gorse.DBItem

	// Name from the rss_feed table.
	FeedName string

	// Read state from rss_item_state table
	ReadState string
}

// connectToDB opens a new connection to the database.
func connectToDB(settings *Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s connect_timeout=10",
		settings.DBUser, settings.DBPass, settings.DBName, settings.DBHost)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Printf("Failed to connect to the database: %s", err)
		return nil, err
	}

	log.Print("Opened new connection to the database.")
	return db, nil
}

// getDB connects us to the database if necessary, and returns an active
// database connection.
//
// We use the global DB variable to try to ensure we use a single connection.
func getDB(settings *Config) (*sql.DB, error) {
	// If we have a db connection, ensure that it is still available so that we
	// reconnect if it is not.
	if DB != nil {
		err := DB.Ping()
		if err == nil {
			return DB, nil
		}

		log.Printf("Database ping failed: %s", err)

		// Continue on, but set us so that we attempt to reconnect.

		DBLock.Lock()
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
		DBLock.Unlock()
	}

	DBLock.Lock()
	defer DBLock.Unlock()

	if DB != nil {
		return DB, nil
	}

	db, err := connectToDB(settings)
	if err != nil {
		log.Printf("Failed to connect to the database: %s", err)
		return nil, err
	}

	// Set global
	DB = db

	return DB, nil
}

// dbCountItems retrieves a count of items.
//
// This is for pagination.
func dbCountItems(db *sql.DB, userID int, state gorse.ReadState) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM rss_item ri
		LEFT JOIN rss_feed rf ON rf.id = ri.rss_feed_id
		LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE rf.active = true AND
			COALESCE(ris.state, 'unread') = $1 AND
			COALESCE(ris.user_id, $2) = $3
`

	rows, err := db.Query(query, state.String(), userID, userID)
	if err != nil {
		return -1, err
	}

	if !rows.Next() {
		return -1, errors.New("count not found")
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		_ = rows.Close()
		return -1, err
	}

	if err := rows.Close(); err != nil {
		return -1, fmt.Errorf("problem closing rows: %s", err)
	}

	return count, nil
}

// dbRetrieveFeedItems retrieves feed items from the database which are marked
// a given state.
func dbRetrieveFeedItems(db *sql.DB, settings *Config, order sortOrder,
	page, userID int, state gorse.ReadState) ([]DBItem, error) {

	if page < 1 {
		return nil, errors.New("invalid page number")
	}

	query := `
		SELECT
			rf.name,
			ri.id,
			ri.title,
			ri.link,
			ri.description,
			ri.publication_date
		FROM rss_item ri
		JOIN rss_feed rf ON rf.id = ri.rss_feed_id
		LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE rf.active = true AND
			COALESCE(ris.state, 'unread') = $1 AND
			COALESCE(ris.user_id, $2) = $2
`

	if order == sortAscending {
		query += "ORDER BY ri.publication_date ASC, rf.name, ri.title"
	} else {
		query += "ORDER BY ri.publication_date DESC, rf.name, ri.title"
	}

	query += " LIMIT $3 OFFSET $4"

	offset := (page - 1) * pageSize

	rows, err := db.Query(query, state.String(), userID, pageSize, offset)
	if err != nil {
		return nil, err
	}

	var items []DBItem
	for rows.Next() {
		item := DBItem{}

		err := rows.Scan(&item.FeedName, &item.ID, &item.Title, &item.Link,
			&item.Description, &item.PublicationDate)
		if err != nil {
			log.Printf("Failed to scan row information: %s", err)
			_ = rows.Close()
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failure fetching rows: %s", err)
	}

	return items, nil
}

// Retrieve an item's information from the database. This includes the item's
// state for the given user.
func dbGetItem(db *sql.DB, itemID int64, userID int) (DBItem, error) {
	query := `
		SELECT
			ri.id,
			ri.title,
			ri.description,
			ri.link,
			ri.publication_date,
			ri.guid,
			ri.rss_feed_id,
			rf.name,
			COALESCE(ris.state, 'unread')
		FROM rss_item ri
		JOIN rss_feed rf ON ri.rss_feed_id = rf.id
		LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE ri.id = $1 AND
			COALESCE(ris.user_id, $2) = $3
`
	rows, err := db.Query(query, itemID, userID, userID)
	if err != nil {
		return DBItem{}, err
	}

	for rows.Next() {
		item := DBItem{}

		if err := rows.Scan(&item.ID, &item.Title, &item.Description, &item.Link,
			&item.PublicationDate, &item.GUID, &item.RSSFeedID, &item.FeedName,
			&item.ReadState); err != nil {
			_ = rows.Close()
			return DBItem{}, fmt.Errorf("failed to scan row: %s", err)
		}

		if err := rows.Close(); err != nil {
			return DBItem{}, fmt.Errorf("error closing rows: %s", err)
		}

		return item, nil
	}

	if err := rows.Err(); err != nil {
		return DBItem{}, fmt.Errorf("failure fetching rows: %s", err)
	}

	return DBItem{}, fmt.Errorf("item not found")
}

// Record the item was read after having been saved to read later.
//
// It is useful to be able to refer back to such items as it is likely they were
// looked at more closely than others.
func dbRecordReadAfterReadLater(db *sql.DB, userID int, item DBItem) error {
	query := `
		INSERT INTO rss_item_read_after_archive
		(user_id, rss_feed_id, rss_item_id)
		VALUES ($1, $2, $3)
`
	if _, err := db.Exec(query, userID, item.RSSFeedID, item.ID); err != nil {
		return fmt.Errorf("unable to insert: %s", err)
	}

	return nil
}
