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
	"github.com/horgh/gorse"
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
	LastUpdateTime *time.Time

	// Whether the feed is set to archive mode. Archive mode means that new items
	// get recorded but set to read automatically. I find this useful for feeds I
	// don't actively ever look at, but want to track them in case I need to at
	// some point. For example, a feed I usually read through a different web
	// interface, but if I fall behind on that web interface and can't go back far
	// enough, then I might need to look at it through Gorse.
	Archive bool
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
id, name, uri, update_frequency_seconds, last_update_time, archive
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
		var nt pq.NullTime

		if err := rows.Scan(&feed.ID, &feed.Name, &feed.URI,
			&feed.UpdateFrequencySeconds, &nt, &feed.Archive); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("failed to scan row: %s", err)
		}

		if nt.Valid {
			feed.LastUpdateTime = &nt.Time
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
		if !shouldUpdateFeed(config, &feed, ignorePollTimes) {
			continue
		}

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

// Check if we need to update. We may be always forcing an update. If not, we
// decide based on when we last updated the feed.
func shouldUpdateFeed(config *Config, feed *DBFeed, ignorePollTimes bool) bool {
	// Poll no matter what.
	if ignorePollTimes {
		return true
	}

	// Never updated.
	if feed.LastUpdateTime == nil {
		return true
	}

	timeSince := time.Since(*feed.LastUpdateTime)

	if int64(timeSince.Seconds()) < feed.UpdateFrequencySeconds {
		return false
	}

	return true
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

	// Determine when we accept items starting from. See shouldRecordItem() for
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
		// Sanity check the item's information. We require at least a link to be
		// set. Description may be blank. We also permit title to be blank. Per spec
		// all item elements are optional.
		if item.Link == "" {
			log.Printf("Feed: %s: Item has blank link: %s", feed.Name, item.Title)
			continue
		}

		recorded, err := recordFeedItem(config, db, feed, &item, cutoffTime,
			ignorePublicationTimes)
		if err != nil {
			return fmt.Errorf(
				"failed to record feed item title [%s] for feed [%s]: %s",
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
	// that we missed some through not polling frequently enough.
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
// If we have no items yet then it's the zero time.
//
// See shouldRecordItem() for a more in depth explanation of why.
func getFeedCutoffTime(db *sql.DB, feed *DBFeed) (time.Time, error) {
	query := `
		SELECT MAX(t) FROM (
			SELECT MAX(publication_date) AS t
			FROM rss_item WHERE rss_feed_id = $1

			UNION

			SELECT MAX(publication_date) AS t
			FROM rss_item_archive WHERE rss_feed_id = $2
		) u
`

	rows, err := db.Query(query, feed.ID, feed.ID)
	if err != nil {
		return time.Time{},
			fmt.Errorf("failed to query for newest publication date: %s", err)
	}

	// Our default is the zero time if we have no items.
	var newestTime time.Time

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
func recordFeedItem(config *Config, db *sql.DB, feed *DBFeed, item *rss.Item,
	cutoffTime time.Time, ignorePublicationTimes bool) (bool, error) {
	record, err := shouldRecordItem(config, db, feed, item, cutoffTime,
		ignorePublicationTimes)
	if err != nil {
		return false, fmt.Errorf("unable to decide whether to record item: %s", err)
	}

	if !record {
		return false, nil
	}

	query := `
INSERT INTO rss_item
(title, description, link, publication_date, rss_feed_id, guid)
VALUES($1, $2, $3, $4, $5, $6)
RETURNING id
`

	var guid *string
	if item.GUID != "" {
		guid = &item.GUID
	}
	params := []interface{}{item.Title, item.Description, item.Link, item.PubDate,
		feed.ID, guid}

	rows, err := db.Query(query, params...)
	if err != nil {
		return false, fmt.Errorf("failed to add item with title [%s]: %s",
			item.Title, err)
	}

	var id int64

	for rows.Next() {
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("failed to scan row: %s", err)
		}
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failure fetching rows: %s", err)
	}

	// On first poll we set all items polled as read. Otherwise when adding a feed
	// we get a bunch of old items all at once which is not very nice.
	//
	// Also if the feed is set to archive mode then it goes directly to read.
	if feed.LastUpdateTime == nil || feed.Archive {
		// We are currently single user.
		userID := 1
		if err := gorse.DBSetItemReadState(db, id, userID, gorse.Read); err != nil {
			return false, fmt.Errorf("failure setting item read state: %s", err)
		}
	}

	if config.Quiet == 0 {
		log.Printf("Added item with title [%s] to feed [%s]", item.Title, feed.Name)
	}

	return true, nil
}

// Decide whether we should record the feed item into the database.
//
// If we've never polled a feed yet then we always need to record it.
//
// If the item has a GUID (an optional element that I don't require), rely on
// it alone to decide whether to include the item. If there's an item with the
// GUID then it's recorded, otherwise it needs to be recorded.
//
// I choose to place trust in the GUID as I encountered a case where a feed
// added items prior to the date requirements described below. This meant the
// item was not recorded but should have been. If a feed uses GUIDs correctly
// my thinking is that they avoid the issues around link changes (where a feed
// may mass update links such as because of a domain/protocol change). This is
// probably optimistic, but I'm trying it!
//
// If the item has no GUID then we decide by the item's link and publication
// date.
//
// Check whether we have an item with this link already. It is optional for an
// element to have a link, but it's something I require. If we have an item
// with the link, then it's already recorded. If we don't, then decide by the
// item's publication date.
//
// The item's publication date must be on or after the cut off time. The cut
// off time is the publication date of the newest item we have from the feed.
//
// We skip items based on publication date because occasionally feeds mass
// update their links. If we go by link alone then we end up recording items
// that were already recorded. Consider the case where a feed switches to https
// links.
//
// In the past I also relied on the cut off time to avoid adding lots of items
// when first adding a feed. Instead on first poll I now record all items but
// flag them as read, so the cut off time isn't necessary for this case.
func shouldRecordItem(config *Config, db *sql.DB, feed *DBFeed, item *rss.Item,
	cutoffTime time.Time, ignorePublicationTimes bool) (bool, error) {
	// Never polled the feed yet? By definition then we need to record all its
	// items.
	if feed.LastUpdateTime == nil {
		return true, nil
	}

	// If the item has a GUID then rely on it. Don't bother checking anything
	// else.
	if item.GUID != "" {
		exists, err := feedItemExistsByGUID(db, feed, item)
		if err != nil {
			return false, fmt.Errorf("failed to check if item exists by guid: %s",
				err)
		}

		if exists {
			return false, nil
		}

		// It's possible the item's link is in the database. There is a unique
		// constraint on links. We've not always tracked GUID, so items prior to
		// doing so will appear to not be present but may be. Deal with this.
		found, err := fixOldItem(db, feed, item)
		if err != nil {
			return false, fmt.Errorf("failed to fix old item: %s", err)
		}

		if found {
			return false, nil
		}

		return true, nil
	}

	exists, err := feedItemExistsByLink(db, feed, item)
	if err != nil {
		return false, fmt.Errorf("failed to check if item exists by link: %s", err)
	}

	if exists {
		return false, nil
	}

	// It looks like we don't have it stored. Decide whether we should store it
	// based on when it was published.

	if ignorePublicationTimes {
		return true, nil
	}

	if item.PubDate.Before(cutoffTime) {
		// I want to always log that this happened, not only in verbose mode. I want
		// to see if there are items that are missed due to using a hard cutoff as
		// I may need to reconsider it if so.
		log.Printf(
			"Skipping recording item from feed [%s] due to its publication time (%s, cutoff time is %s): %s: %s",
			feed.Name, item.PubDate, cutoffTime, item.Title, item.Link)
		return false, nil
	}

	return true, nil
}

// feedItemExistsByGUID checks if there is an item in the database for this feed
// with its GUID.
func feedItemExistsByGUID(db *sql.DB, feed *DBFeed,
	item *rss.Item) (bool, error) {
	// Check main table.

	query := `SELECT id FROM rss_item WHERE rss_feed_id = $1 AND guid = $2`
	count, err := countRowsProduced(db, query, feed.ID, item.GUID)
	if err != nil {
		return false, fmt.Errorf("unable to query rss_item: %s", err)
	}

	if count > 0 {
		return true, nil
	}

	// Check archive table.

	query = `SELECT id FROM rss_item_archive WHERE rss_feed_id = $1 AND guid = $2`
	count, err = countRowsProduced(db, query, feed.ID, item.GUID)
	if err != nil {
		return false, fmt.Errorf("unable to query rss_item_archive: %s", err)
	}

	return count > 0, nil
}

// feedItemExistsByLink checks if there is an item in the database for this feed
// with its URL.
func feedItemExistsByLink(db *sql.DB, feed *DBFeed,
	item *rss.Item) (bool, error) {
	// Check main table.

	query := `SELECT id FROM rss_item WHERE rss_feed_id = $1 AND link = $2`
	count, err := countRowsProduced(db, query, feed.ID, item.Link)
	if err != nil {
		return false, fmt.Errorf("unable to query rss_item: %s", err)
	}

	if count > 0 {
		return true, nil
	}

	// Check archive table.

	query = `SELECT id FROM rss_item_archive WHERE rss_feed_id = $1 AND link = $2`
	count, err = countRowsProduced(db, query, feed.ID, item.Link)
	if err != nil {
		return false, fmt.Errorf("unable to query rss_item_archive: %s", err)
	}

	return count > 0, nil
}

// We want to record the item in the database since we've found its GUID is not
// present.
//
// However, since we've not always tracked GUIDs, it's possible the item is
// there and we just don't have its GUID.
//
// Look up the item by link (it's a database constraint that the link be to a
// feed unique).
//
// Return whether we found an item to update. This information is only useful if
// there was no error.
func fixOldItem(db *sql.DB, feed *DBFeed, item *rss.Item) (bool, error) {
	dbItem, err := gorse.FindItemByLink(db, feed.ID, item.Link)
	if err != nil {
		// If it's not present with this link then there's nothing to fix.
		if err == gorse.ErrItemNotFound {
			return false, nil
		}

		return false, fmt.Errorf("error looking up item by link: %s", err)
	}

	// It's in the database. We need to know whether it's missing a GUID or if it
	// has one different than expected. If it's just missing a GUID then we can
	// set one. If it has one that's different then that is surprising. Report
	// it.

	if dbItem.GUID == nil {
		if err := dbSetItemGUID(db, dbItem.ID, item.GUID); err != nil {
			return false, fmt.Errorf("unable to set GUID: %s", err)
		}

		return true, nil
	}

	// This should not happen. We should only call this function when we've found
	// no item with the GUID.
	if *dbItem.GUID == item.GUID {
		return false, fmt.Errorf("item found by GUID, but this shouldn't happen")
	}

	return false, fmt.Errorf("mismatching GUID for item")
}

func dbSetItemGUID(db *sql.DB, id int64, guid string) error {
	query := `UPDATE rss_item SET guid = $1 WHERE id = $2`

	if _, err := db.Exec(query, guid, id); err != nil {
		return fmt.Errorf("failed to set guid: %s", err)
	}

	return nil
}

// Execute a query and count how many rows returned.
func countRowsProduced(db *sql.DB, query string,
	params ...interface{}) (int, error) {
	rows, err := db.Query(query, params...)
	if err != nil {
		return -1, fmt.Errorf("query failed: %s", err)
	}

	count := 0
	for rows.Next() {
		count++
	}

	if err := rows.Err(); err != nil {
		return -1, fmt.Errorf("failure fetching rows: %s", err)
	}

	return count, nil
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
