// Package gorselib provides helper function for interacting with RSS, RDF,
// and Atom feeds. Primarily this surrounds building and reading/parsing.
package gorselib

import (
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"log"
	"regexp"
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

// RSSFeed describes a feed. This is the type that callers will generally work
// with if the input/output format is not important.
type RSSFeed struct {
	// Database id of the feed.
	ID int64

	// Name of the feed.
	Name string

	// Description of the fed.
	Description string

	// URI to the feed.
	URI string

	// Update frequency in seconds.
	UpdateFrequencySeconds int64

	// Last time we updated.
	LastUpdateTime time.Time

	Items []RSSItem
}

// RSSItem describes an item in a feed. This is the type that callers will
// generally work with if the input/output format is not important.
type RSSItem struct {
	FeedID      int64
	FeedName    string
	ID          int64
	Title       string
	Description string
	// HTML version of description. TODO: This is only used in gorse.
	DescriptionHTML       template.HTML
	URI                   string
	PublicationDate       time.Time
	PublicationDateString string
	ReadState             string
}

// SetQuiet controls the gorselib setting 'Quiet'.
func SetQuiet(quiet bool) {
	config.Quiet = quiet
}

// SanitiseItemText takes text (e.g., title or description) and removes any HTML
// markup. This is because some feeds (e.g., Slashdot) include a lot of markup
// I don't want to actually show.
//
// We also decode HTML entities since apparently we can get these through to
// this point (they will be encoded again as necessary when we render the
// page).
//
// For example in a raw XML from Slashdot we have this:
//
// <item><title>AT&amp;amp;T Gets Patent To Monitor and Track File-Sharing Traffic</title>
//
// Which gets placed into the database as:
// AT&amp;T Gets Patent To Monitor and Track File-Sharing Traffic
//
// This can be used to take any string which has HTML in it to clean up that
// string and make it non-HTML.
//
// While elements such as 'title' can have HTMLin them, this seems applied
// inconsistently. For instance, consider this title from a Slashdot feed:
//
// <title>Google Maps Updated With Skyfall&lt;/em&gt; Island Japan Terrain</title>
//
// That is: </em> in there but no <em>.
//
// In the database this is present as </em>.
//
// Thus we do not place the HTML into the page raw.
func SanitiseItemText(text string) (string, error) {
	// First remove raw HTML.
	re, err := regexp.Compile("(?s)<.*?>")
	if err != nil {
		log.Printf("Failed to compile html regexp: %s", err)
		return text, err
	}
	text = re.ReplaceAllString(text, "")

	// Decode HTML entities.
	text = html.UnescapeString(text)

	// Turn any multiple spaces into a single space.
	re, err = regexp.Compile("(?s)\\s+")
	if err != nil {
		log.Printf("Failed to compile whitespace regexp: %s", err)
		return text, err
	}
	text = re.ReplaceAllString(text, " ")

	return text, nil
}

// RetrieveFeeds finds RSS feeds from the database.
func RetrieveFeeds(db *sql.DB) ([]RSSFeed, error) {
	query := `
SELECT
id, name, uri, update_frequency_seconds, last_update_time
FROM rss_feed
WHERE active = true
ORDER BY name
`
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Failed to query for feeds from the database: %s", err)
		return nil, err
	}

	var feeds []RSSFeed

	for rows.Next() {
		var id int64
		var name string
		var uri string
		var updateFrequencySeconds int64
		var lastUpdateTime time.Time

		err := rows.Scan(&id, &name, &uri, &updateFrequencySeconds,
			&lastUpdateTime)
		if err != nil {
			log.Printf("Failed to retrieve row data: %s", err)
			_ = rows.Close()
			return nil, err
		}

		var feed = RSSFeed{
			ID:   id,
			Name: name,
			URI:  uri,
			UpdateFrequencySeconds: updateFrequencySeconds,
			LastUpdateTime:         lastUpdateTime,
		}

		feeds = append(feeds, feed)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("failure fetching rows: %s", err)
	}

	return feeds, nil
}

// GetDurationUntilNextUpdateForDisplay builds a string describing how long
// until the next update. For example, '2 hours'
func (feed RSSFeed) GetDurationUntilNextUpdateForDisplay() (string, error) {
	// We want to know how long since our last update as we will compare this
	// with how long our update frequency is.
	timeSinceLastUpdate := time.Now().Sub(feed.LastUpdateTime)

	// Take our update frequency and subtract the time since our last update to
	// find the duration until the next update.
	nextUpdateTimeSeconds := float64(feed.UpdateFrequencySeconds) -
		timeSinceLastUpdate.Seconds()

	minutes := nextUpdateTimeSeconds / 60
	hours := int(minutes / 60)
	minutesInt := int(minutes)

	// It is possible for us to be past the time where we should have updated (if
	// update script has not run, or there is a problem). Just say that we should
	// be updating now/next time.
	if nextUpdateTimeSeconds < 0 {
		return "the next update", nil
	}

	if hours > 0 {
		if hours == 1 {
			return "1 hour", nil
		}
		return fmt.Sprintf("%d hours", hours), nil
	}

	if minutesInt == 1 {
		return "1 minute", nil
	}

	return fmt.Sprintf("%d minutes", minutesInt), nil
}

// GetDurationSinceUpdateForDisplay builds a string describing how long ago the
// last update was. For example, '2 hours'.
func (feed RSSFeed) GetDurationSinceUpdateForDisplay() string {
	// Now minus last update time.
	difference := time.Now().Sub(feed.LastUpdateTime)

	// Hours and minutes are given as floating. Round them.
	hours := int(difference.Hours())
	minutes := int(difference.Minutes())
	if hours > 0 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}

	if minutes == 1 {
		return "1 minute"
	}

	return fmt.Sprintf("%d minutes", minutes)
}

// GetUpdateFrequencyForDisplay builds a string suitable for the user out of our
// update frequency seconds.
func (feed RSSFeed) GetUpdateFrequencyForDisplay() string {
	updateMinutes := feed.UpdateFrequencySeconds / 60
	updateHours := updateMinutes / 60

	// We give a rough idea of the time rather than being too specific.
	//
	// For example if we have over an hour then we just say there are x hour(s)
	// and say nothing about the minute(s).
	var updateString string
	if updateHours > 0 {
		if updateHours == 1 {
			updateString = "1 hour"
		} else {
			updateString = fmt.Sprintf("%d hours", updateHours)
		}
	} else {
		if updateMinutes == 1 {
			updateString = "1 minute"
		} else {
			updateString = fmt.Sprintf("%d minutes", updateMinutes)
		}
	}

	return updateString
}
