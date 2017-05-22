// RSS feed poller.
//
// This program works roughly as follows:
// - Find RSS feeds from a database.
// - For every RSS feed, if it was last fetched less than its update frequency
//   ago, retrieve it and then record that a retrieval was done.
// - For every item fetched from the feed, add information about that item into
//   the database (if it's not there yet).
//
// This program is intended to be run periodically through something like cron.
//
// We try to ensure that we do not poll the RSS feeds too much by recording a
// last update time and update frequency if the feed includes such data.
package main

import (
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/horgh/config"
	"github.com/horgh/rss"
	"github.com/lib/pq"
)

// Config holds runtime configuration info.
type Config struct {
	DBUser string
	DBPass string
	DBName string
	DBHost string
	Quiet  int64
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

func main() {
	singleFeed := flag.String("feed-name", "",
		"Single feed name to process. Process all feeds if not given.")
	configPath := flag.String("config", "", "Path to the configuration file.")
	ignorePollTimes := flag.Bool("ignore-poll-times", false, "Ignore the last polled times. This causes us to poll feeds even if we recently polled them.")
	ignorePublicationTimes := flag.Bool("ignore-publication-times", false, "Ignore publication times. Normally we filter items from a feed to only record items since the last we've seen. Enabling this option causes us to record items based only on whether we've seen their URL.")

	flag.Parse()

	if len(*configPath) == 0 {
		log.Print("You must specify a configuration file.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var settings Config
	if err := config.GetConfig(*configPath, &settings); err != nil {
		log.Fatalf("Failed to retrieve config: %s", err)
	}

	log.SetFlags(log.Ltime)

	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s connect_timeout=10",
		settings.DBUser, settings.DBPass, settings.DBName, settings.DBHost)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Database close: %s", err)
		}
	}()

	if settings.Quiet == 0 {
		rss.SetVerbose(true)
	}

	// Retrieve our feeds from the database.
	feeds, err := retrieveFeeds(db)
	if err != nil {
		log.Fatalf("Failed to retrieve feeds: %s", err)
	}

	// Are we limiting this run to one feed? If so, find it and make a new slice
	// with only this feed in it.
	if len(*singleFeed) > 0 {
		feedsSingle := []DBFeed{}
		for _, feed := range feeds {
			if feed.Name == *singleFeed {
				feedsSingle = append(feedsSingle, feed)
				break
			}
		}

		if len(feedsSingle) == 0 {
			log.Fatalf("Feed with name [%s] not found!", *singleFeed)
		}

		if settings.Quiet == 0 {
			log.Printf("Using only feed [%s]", *singleFeed)
		}

		feeds = feedsSingle
	}

	if err := processFeeds(&settings, db, feeds, *ignorePollTimes,
		*ignorePublicationTimes); err != nil {
		log.Fatal("Failed to process feed(s)")
	}
}

// retrieveFeeds finds feeds from the database.
func retrieveFeeds(db *sql.DB) ([]DBFeed, error) {
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

		if err := rows.Scan(&feed.ID, &feed.Name, &feed.URI,
			&feed.UpdateFrequencySeconds, &feed.LastUpdateTime); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("failed to scan row: %s", err)
		}

		feeds = append(feeds, feed)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failure fetching rows: %s", err)
	}

	return feeds, nil
}

// processFeeds processes each feed in turn.
//
// We look at every feed, and retrieve it if it needs to be updated.
//
// We store the new retrieved information and update the feed's details if we
// retrieved it.
//
// If there was an error, we return an error, otherwise we return nil.
func processFeeds(config *Config, db *sql.DB, feeds []DBFeed,
	ignorePollTimes, ignorePublicationTimes bool) error {

	feedsUpdated := 0

	for _, feed := range feeds {
		// Check if we need to update. We may be always forcing an update. If not,
		// we decide based on when we last updated the feed.
		if !ignorePollTimes {
			timeSince := time.Since(feed.LastUpdateTime)

			if config.Quiet == 0 {
				log.Printf("Feed [%s] was updated [%d] second(s) ago, and stored update frequency is %d second(s).",
					feed.Name, int64(timeSince.Seconds()), feed.UpdateFrequencySeconds)
			}

			if int64(timeSince.Seconds()) < feed.UpdateFrequencySeconds {
				if config.Quiet == 0 {
					log.Print("Skipping update.")
				}
				continue
			}
		}

		// Perform our update.

		if config.Quiet == 0 {
			log.Printf("Updating feed [%s]", feed.Name)
		}

		// Track when we update the feed. We want a time just before we do so as we
		// will only accept items after this time next time. This is the time when
		// we poll.
		updateTime := time.Now()

		if err := updateFeed(config, db, &feed,
			ignorePublicationTimes); err != nil {
			log.Printf("Failed to update feed: %s: %s", feed.Name, err)
			continue
		}

		if config.Quiet == 0 {
			log.Printf("Updated feed [%s]", feed.Name)
		}

		// Record that we have performed an update of this feed. Do this after we
		// have successfully updated the feed so as to ensure we try repeatedly in
		// case of transient errors e.g. if network is down.
		if err := recordFeedUpdate(db, &feed, updateTime); err != nil {
			return fmt.Errorf("failed to record update on feed [%s]: %s", feed.Name,
				err)
		}

		feedsUpdated++
	}

	if config.Quiet == 0 {
		log.Printf("Updated %d/%d feed(s).", feedsUpdated, len(feeds))
	}

	return nil
}

// updateFeed fetches, parses, and stores the new items in a feed.
//
// We should have already determined we need to perform an update.
func updateFeed(config *Config, db *sql.DB, feed *DBFeed,
	ignorePublicationTimes bool) error {
	// Retrieve and parse the feed body (XML, generally).

	xmlData, err := retrieveFeed(feed)
	if err != nil {
		return fmt.Errorf("failed to retrieve feed: %s", err)
	}

	if err := storeFeedPayload(db, feed, xmlData); err != nil {
		return fmt.Errorf("unable to store payload to database: %s", err)
	}

	channel, err := rss.ParseFeedXML(xmlData)
	if err != nil {
		return fmt.Errorf("failed to parse XML of feed: %s", err)
	}

	if config.Quiet == 0 {
		log.Printf("Fetched %d item(s) for feed [%s]", len(channel.Items), feed.Name)
	}

	// Determine when we accept items starting from. See recordFeedItem() for
	// more information on this.
	cutoffTime, err := getFeedCutoffTime(db, feed)
	if err != nil {
		return fmt.Errorf("unable to determine feed cutoff time: %s: %s", feed.Name,
			err)
	}

	if config.Quiet == 0 {
		log.Printf("Feed [%s] cutoff time: %s", feed.Name, cutoffTime)
	}

	// Record each item in the feed.

	recordedCount := 0
	for _, item := range channel.Items {
		recorded, err := recordFeedItem(config, db, feed, &item, cutoffTime,
			ignorePublicationTimes)
		if err != nil {
			return fmt.Errorf("failed to record feed item title [%s] for feed [%s]: %s",
				item.Title, feed.Name, err)
		}

		if recorded {
			recordedCount++
		}
	}

	if config.Quiet == 0 {
		log.Printf("Added %d/%d item(s) from feed [%s]", recordedCount,
			len(channel.Items), feed.Name)
	}

	// Log if we recorded all items we received. Why? Because this may indicate
	// that we missed some through not updating frequently enough.
	if recordedCount == len(channel.Items) {
		log.Printf("Warning: recorded all items from feed [%s] (%d/%d)", feed.Name,
			recordedCount, len(channel.Items))
	}

	return nil
}

// retrieveFeed fetches the raw feed content.
func retrieveFeed(feed *DBFeed) ([]byte, error) {
	// Retrieve the feed via an HTTP call.

	// NOTE: We set up a http.Transport to use TLS settings. Then we set the
	// transport on the http.Client, and then make the request.
	//
	// We have to do it in this round about way rather than simply http.Get()
	// or the like in order to pass through the TLS setting it appears.
	//
	// I don't actually have any TLS settings any more. I used to disable
	// verification (one of my sites had a valid certificate).

	tlsConfig := &tls.Config{}

	httpTransport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   time.Second * 10,
	}

	httpResponse, err := httpClient.Get(feed.URI)
	if err != nil {
		return nil, fmt.Errorf("HTTP request for feed failed. (%s): %s", feed.Name,
			err)
	}

	defer func() {
		if err := httpResponse.Body.Close(); err != nil {
			log.Printf("HTTP response body close: %s", err)
		}
	}()

	// While we will be decoding XML, and the XML package can read directly from
	// an io.Reader, I read it all in here for simplicity so that this fetch
	// function does not need to worry about anything to do with XML.
	body, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP body: %s", err)
	}

	return body, nil
}

// Store the feed's payload, typically XML, into the database.
//
// We track the latest payload each time we fetch it. This is mainly so that I
// have a sample set to examine/test with.
//
// It is possible the payload isn't a valid feed at this point or that we could
// not process it. This is intentional. I want to be able to inspect the payload
// if it failed.
func storeFeedPayload(db *sql.DB, feed *DBFeed, payload []byte) error {
	query := `UPDATE rss_feed SET last_payload = $1 WHERE id = $2`

	if _, err := db.Exec(query, payload, feed.ID); err != nil {
		return fmt.Errorf("failed to record payload for feed ID [%d] name [%s]: %s",
			feed.ID, feed.Name, err)
	}

	return nil
}

// Determine the time after which we will accept items from this feed.
//
// If we have at least one item from the feed already, then this time is the
// most recent item's publication time.
//
// If we have no items yet, use the feed's last poll time.
//
// See recordFeedItem() for a more in depth explanation of why.
func getFeedCutoffTime(db *sql.DB, feed *DBFeed) (time.Time, error) {
	query := `SELECT MAX(publication_date) FROM rss_item WHERE rss_feed_id = $1`

	rows, err := db.Query(query, feed.ID)
	if err != nil {
		return time.Time{},
			fmt.Errorf("failed to query for newest publication date: %s", err)
	}

	// Our default is the last poll time if there is no item yet.
	newestTime := feed.LastUpdateTime

	for rows.Next() {
		// We get null time if there's no item.
		var nt pq.NullTime

		if err := rows.Scan(&nt); err != nil {
			_ = rows.Close()
			return time.Time{}, fmt.Errorf("failed to scan row: %s", err)
		}

		if !nt.Valid {
			continue
		}

		newestTime = nt.Time
	}

	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("failure fetching rows: %s", err)
	}

	return newestTime, nil
}

// recordFeedItem inserts the feed item into the database.
//
// Return whether we actually performed an insert and if there was an error.
//
// We store the item if:
//
// - The item is not yet in the database
//
// - The item's publication date meets our date requirements.
//
// The date requirements are: The item's publication date must be on or after
// the date of the newest item we have from the feed. If we have no items from
// the feed yet, this date will be the last time we have recorded for polling
// the feed.
//
// The latter is so that when we first add a feed that we do not end up adding
// many old items. When adding the feed, we say we last polled it at the time
// of adding it, so we get items starting from that point.
//
// My previous choice of timing requirement was problematic. It was to only
// accept items that were older than the last time we polled the feed. This was
// problematic because a feed could have been published after we last polled it
// and contain items prior to us polling it, such as if it collected a batch of
// items every hour for example, and we happened to poll it halfway through the
// hour.
//
// We skip items based on publication date for a couple reasons:
//
// - First, when we first add a feed, we don't want a large number of items to
//   suddenly appear into people's feed. In some cases this can lead to 50-100
//   items going back a long time.
//
// - Second, occasionally feed items IDs/links change, and this too can lead to
//   a large number of items appearing needing to be stored, again leading to
//   polluted feeds. For this we could also restrict based on GUID, but that is
//   an optional element in an item.
//
// Previously I did not take publication date into account, and simply added
// based on whether the feed's URL was stored yet. This had problems related to
// when a feed's URLs change, and also when I first added a feed it would mean
// a large number of items all at once.
func recordFeedItem(config *Config, db *sql.DB, feed *DBFeed, item *rss.Item,
	cutoffTime time.Time, ignorePublicationTimes bool) (bool, error) {
	// Sanity check the item's information. We require at least a link to be set.
	// Description may be blank. We also permit title to be blank.
	if item.Link == "" {
		return false, fmt.Errorf("item has blank link: %s", item.Title)
	}

	// If the item is already recorded, then we don't do anything.
	exists, err := feedItemExists(db, feed, item)
	if err != nil {
		return false, fmt.Errorf("failed to check if feed item title [%s] exists: %s",
			item.Title, err)
	}

	if exists {
		if config.Quiet == 0 {
			log.Printf("Skipping recording item from feed [%s] due to having it already: %s",
				feed.Name, item.Title)
		}
		return false, nil
	}

	// It looks like we don't have it stored. Decide whether we should store it
	// based on when it was published.

	if !ignorePublicationTimes && item.PubDate.Before(cutoffTime) {
		if config.Quiet == 0 {
			log.Printf("Skipping recording item from feed [%s] due to its publication time (%s, cutoff time is %s): %s",
				feed.Name, item.PubDate, cutoffTime, item.Title)
		}
		return false, nil
	}

	// We need to record this item.

	query := `
INSERT INTO rss_item
(title, description, link, publication_date, rss_feed_id)
VALUES($1, $2, $3, $4, $5)
`
	if _, err := db.Exec(query, item.Title, item.Description, item.Link,
		item.PubDate, feed.ID); err != nil {
		return false, fmt.Errorf("failed to add item with title [%s]: %s",
			item.Title, err)
	}

	if config.Quiet == 0 {
		log.Printf("Added item with title [%s] to feed [%s]", item.Title, feed.Name)
	}

	return true, nil
}

// feedItemExists checks if this item is already recorded in the database.
//
// It does this by checking if the uri exists for the given feed id.
func feedItemExists(db *sql.DB, feed *DBFeed,
	item *rss.Item) (bool, error) {
	// Check main table.
	query := `SELECT id FROM rss_item WHERE rss_feed_id = $1 AND link = $2`
	rows, err := db.Query(query, feed.ID, item.Link)
	if err != nil {
		return false,
			fmt.Errorf("failed to check if item title [%s] exists for feed [%s]: %s",
				item.Title, feed.Name, err)
	}

	// If we have a row, then the item exists.

	count := 0
	for rows.Next() {
		count++
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failure fetching rows: %s", err)
	}

	if count > 0 {
		return true, nil
	}

	// Check archive table.
	query = `SELECT id FROM rss_item_archive WHERE rss_feed_id = $1 AND link = $2`
	rows, err = db.Query(query, feed.ID, item.Link)
	if err != nil {
		return false, fmt.Errorf("failed to check if item title [%s] exists for feed [%s]: %s",
			item.Title, feed.Name, err)
	}

	// If we have a row, then the item exists.

	count = 0
	for rows.Next() {
		count++
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failure fetching rows: %s", err)
	}

	if count > 0 {
		return true, nil
	}

	return false, nil
}

// recordFeedUpdate sets the last feed update time.
//
// This is the time we last polled the feed.
func recordFeedUpdate(db *sql.DB, feed *DBFeed, updateTime time.Time) error {
	query := `UPDATE rss_feed SET last_update_time = $1 WHERE id = $2`

	if _, err := db.Exec(query, updateTime, feed.ID); err != nil {
		return fmt.Errorf("failed to record feed update for feed id [%d] name [%s]: %s",
			feed.ID, feed.Name, err)
	}

	return nil
}
