// Package gorselib provides helper function for interacting with RSS, RDF,
// and Atom feeds. Primarily this surrounds building and reading/parsing.
package gorselib

import (
	"database/sql"
	"fmt"
	"time"
)

// Config controls package wide settings.
type Config struct {
	// Control whether we have verbose output (or not).
	Quiet bool
}

// Use a global default set of settings.
//
// See package log for a similar approach (global default settings).
var config = Config{
	Quiet: false,
}

// DBFeed holds the information from the database about a feed.
type DBFeed struct {
	// Database ID.
	ID int64

	// Name.
	Name string

	// URI to the feed.
	URI string

	// Update frequency in seconds.
	UpdateFrequencySeconds int64

	// Last time we updated.
	LastUpdateTime time.Time
}

// SetQuiet controls the gorselib setting 'Quiet'.
func SetQuiet(quiet bool) {
	config.Quiet = quiet
}

// RetrieveFeeds finds RSS feeds from the database.
func RetrieveFeeds(db *sql.DB) ([]DBFeed, error) {
	query := `
SELECT
id, name, uri, update_frequency_seconds, last_update_time
FROM rss_feed
WHERE active = true
ORDER BY name
`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query for feeds: %s", err)
	}

	var feeds []DBFeed

	for rows.Next() {
		feed := DBFeed{}

		err := rows.Scan(&feed.ID, &feed.Name, &feed.URI,
			&feed.UpdateFrequencySeconds, &feed.LastUpdateTime)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("failed to scan row: %s", err)
		}

		feeds = append(feeds, feed)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("failure fetching rows: %s", err)
	}

	return feeds, nil
}
