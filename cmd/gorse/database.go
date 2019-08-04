package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/horgh/gorse"
	"github.com/pkg/errors"
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

func dbCountUnreadItems(
	db *sql.DB,
) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM rss_item ri
		LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE ri.publication_date > NOW() - INTERVAL '1 month' AND ris.state IS NULL
`

	row := db.QueryRow(query)

	var count int
	if err := row.Scan(&count); err != nil {
		return -1, errors.Wrap(err, "error scanning row")
	}

	return count, nil
}

func dbCountReadLaterItems(
	db *sql.DB,
	userID int,
) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM rss_item ri
		JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE ris.user_id = $1 AND ris.state = 'read-later'
`

	row := db.QueryRow(query, userID)

	var count int
	if err := row.Scan(&count); err != nil {
		return -1, errors.Wrap(err, "error scanning row")
	}

	return count, nil
}

func dbRetrieveUnreadItems(
	db *sql.DB,
	settings *Config,
	page int,
) ([]DBItem, error) {
	if page < 1 {
		return nil, errors.New("invalid page number")
	}

	query := `
		SELECT
			ri.id,
			ri.title,
			ri.link,
			ri.description,
			ri.publication_date,
			rf.name
		FROM rss_item ri
		JOIN rss_feed rf ON rf.id = ri.rss_feed_id
		LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
		WHERE ri.publication_date > NOW() - INTERVAL '1 month' AND ris.state IS NULL
		ORDER BY ri.publication_date DESC, rf.name, ri.title
		LIMIT $1 OFFSET $2
`

	rows, err := db.Query(
		query,
		pageSize,
		(page-1)*pageSize,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error querying")
	}

	var items []DBItem
	for rows.Next() {
		var item DBItem
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Link,
			&item.Description,
			&item.PublicationDate,
			&item.FeedName,
		); err != nil {
			_ = rows.Close()
			return nil, errors.Wrap(err, "error scanning row")
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error retrieving rows")
	}

	return items, nil
}

func dbRetrieveReadLaterItems(
	db *sql.DB,
	settings *Config,
	page,
	userID int,
) ([]DBItem, error) {
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
		JOIN rss_item_state ris ON ris.item_id = ri.id
		JOIN rss_feed rf ON rf.id = ri.rss_feed_id
		WHERE ris.user_id = $1 AND ris.state = 'read-later'
		ORDER BY ri.publication_date DESC, rf.name, ri.title
		LIMIT $2 OFFSET $3
`

	rows, err := db.Query(
		query,
		userID,
		pageSize,
		(page-1)*pageSize,
	)
	if err != nil {
		return nil, errors.Wrap(err, "error querying")
	}

	var items []DBItem
	for rows.Next() {
		var item DBItem
		if err := rows.Scan(
			&item.FeedName,
			&item.ID,
			&item.Title,
			&item.Link,
			&item.Description,
			&item.PublicationDate,
		); err != nil {
			_ = rows.Close()
			return nil, errors.Wrap(err, "error scanning row")
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error retrieving rows")
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
