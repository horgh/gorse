/*
 * 2013-06-29
 * will@summercat.com
 *
 * rss fetcher.
 *
 * - find rss feeds from a database.
 * - for every rss feed, if it was last fetched less than its update
 *   frequency ago, record that a retrieval was done, and retrieve
 *   its content.
 * - for every item, add information about that item into the database.
 *
 * this script is intended to be run periodically through something like
 * cron.
 *
 * we try to ensure that we do not poll the rss feeds too much by
 * recording a last update time and update frequency if the feed includes
 * such data.
 */

package main

import (
	"crypto/tls"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	_ "github.com/lib/pq"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"summercat.com/config"
	"summercat.com/gorse/gorselib"
	"time"
)

type GorsePollConfig struct {
	DbUser string
	DbPass string
	DbName string
	DbHost string
	Quiet  int64
}

// retrieveFeed fetches the raw feed content.
func retrieveFeed(feed *gorselib.RssFeed) ([]byte, error) {
	// Retrieve the feed via an HTTP call.

	// NOTE: We set up a http.Transport to use TLS settings (we do not want
	//   to check certificates because my site does not have a valid one
	//   right now), and then set the transport on the http.Client, and then
	//   make the request.
	//
	//   We have to do it in this round about way rather than simply
	//   http.Get() or the like in order to pass through the TLS setting it
	//   appears.
	//
	//   TODO: Enable verification...

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	httpTransport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   time.Second * 10,
	}

	httpResponse, err := httpClient.Get(feed.Uri)

	if err != nil {
		log.Printf("HTTP request for feed failed. (%s): %s", feed.Name, err.Error())

		// It appears we do not need to call Body.Close() here - if we try
		// then we get a runtime error about nil pointer dereference.
		return nil, err
	}

	// while we will be decoding xml, and the xml package can read directly
	// from an io.reader, I read it all in here for simplicity so that this
	// fetch function does not need to worry about anything to do with xml.
	body, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		log.Print("Failed to read all: " + err.Error())
		return nil, err
	}
	return body, nil
}

// feedItemExists checks if this item is already recorded in the database.
// it does this by checking if the uri exists for the given feed id.
func feedItemExists(db *sql.DB, feed *gorselib.RssFeed, item *gorselib.Item) (bool, error) {
	query := `
SELECT id
FROM rss_item
WHERE rss_feed_id = $1
	AND link = $2
`
	rows, err := db.Query(query, feed.Id, item.Link)
	if err != nil {
		log.Printf("Failed to check if item title [%s] exists for feed [%s]: %s",
			item.Title, feed.Name, err.Error())
		return false, err
	}

	// if we have a row, then the item exists.
	count := 0
	for rows.Next() {
		count++
	}
	return count > 0, nil
}

// recordFeedItem inserts the feed item information into the database if it
// is not already present.
// we return whether we actually performed an insert and if there was an
// error.
func recordFeedItem(config *GorsePollConfig, db *sql.DB,
	feed *gorselib.RssFeed, item *gorselib.Item) (bool, error) {
	// sanity check the item's information.
	// we require at least a link to be set.
	// description may be blank. we also permit title to be blank.
	if item.Link == "" {
		log.Printf("Item with title [%s] has no link. Skipping",
			item.Title)
		return false, errors.New("Item has blank link")
	}
	// we need to ensure we have a publication date, and that it is in utc.
	// if we do not have it, we default to using the current time.
	pubDateTime := gorselib.GetItemPubDate(item.PubDate)
	// convert the pub date time to a string suitable for passing to
	// postgres.
	var pubDateDb string = pubDateTime.Format(time.RFC3339)

	// if the item is already recorded, then we don't do anything.
	exists, err := feedItemExists(db, feed, item)
	if err != nil {
		log.Printf("Failed to check if feed item title [%s] exists: %s",
			item.Title, err.Error())
		return false, err
	}
	if exists {
		return false, nil
	}

	// we need to record it.
	var query string = `
INSERT INTO rss_item
(title, description, link, publication_date, rss_feed_id)
VALUES($1, $2, $3, $4, $5)
`
	// TODO: we could check if a single row was affected. the variable
	//   I am dropping here is of type Result which tells us such
	//   information.
	_, err = db.Exec(query, item.Title, item.Description,
		item.Link, pubDateDb, feed.Id)
	if err != nil {
		log.Printf("Failed to add item with title [%s]: %s",
			item.Title, err.Error())
		return false, err
	}
	if config.Quiet == 0 {
		log.Printf("Added item with title [%s] to feed [%s]",
			item.Title, feed.Name)
	}
	return true, nil
}

// updateFeed performs a new request to retrieve the feed.
// we record what items we saw.
// at this point we have determined we need to perform an update.
// we return a nil error if no problems occurred.
func updateFeed(config *GorsePollConfig, db *sql.DB,
	feed *gorselib.RssFeed) error {
	// retrieve the feed body.
	xmlData, err := retrieveFeed(feed)
	if err != nil {
		log.Print("Failed to retrieve feed: " + err.Error())
		return err
	}

	// parse the XML response.
	channel, err := gorselib.ParseFeedXML(xmlData)
	if err != nil {
		return fmt.Errorf("Failed to parse XML of feed: %v", err.Error())
	}

	// record information about each item we parsed.
	if config.Quiet == 0 {
		log.Printf("Fetched %d item(s) for feed [%s]", len(channel.Items),
			feed.Name)
	}
	var recorded_count int = 0
	for _, item := range channel.Items {
		recorded, err := recordFeedItem(config, db, feed, &item)
		if err != nil {
			log.Printf("Failed to record feed item title [%s] for feed [%s]: %s",
				item.Title, feed.Name, err.Error())
			return err
		}
		if recorded {
			recorded_count++
		}
	}
	if config.Quiet == 0 {
		log.Printf("Added %d/%d item(s) from feed [%s]",
			recorded_count, len(channel.Items), feed.Name)
	}

	// raise a message if we had to record all items we received. why? because
	// this may indicate that we missed some through not updating frequently
	// enough.
	if recorded_count == len(channel.Items) {
		log.Printf("Warning: recorded all items from feed [%s] (%d/%d)",
			feed.Name, recorded_count, len(channel.Items))
	}
	return nil
}

// recordFeedUpdate sets the last feed update time to right now.
func recordFeedUpdate(db *sql.DB, feed *gorselib.RssFeed) error {
	var query string = `
UPDATE rss_feed
SET last_update_time = NOW()
WHERE id = $1
`
	_, err := db.Exec(query, feed.Id)
	if err != nil {
		log.Printf("Failed to record feed update for feed id [%d] name [%s]: %s",
			feed.Id, feed.Name, err.Error())
		return err
	}
	feed.LastUpdateTime = time.Now()
	return nil
}

// processFeeds processes each feed in turn.
// we look at every feed, and retrieve it anew if it needs to be updated.
// we store the new retrieved information and update the feed's
// details if we retrieved it.
// if there was an error, we return an error, otherwise we return nil.
func processFeeds(config *GorsePollConfig, db *sql.DB,
	feeds []gorselib.RssFeed, forceUpdate bool) error {

	feedsUpdated := 0

	for _, feed := range feeds {
		// Check if we need to update.
		// We may be always forcing an update.
		// If not, we decide based on when we last updated the feed.
		if !forceUpdate {
			var timeSince time.Duration = time.Since(feed.LastUpdateTime)

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

		err := updateFeed(config, db, &feed)
		if err != nil {
			log.Print("Failed to update feed: " + feed.Name + ": " + err.Error())
			continue
		}

		if config.Quiet == 0 {
			log.Printf("Updated feed [%s]", feed.Name)
		}

		// record that we have performed an update of this feed.
		// do this after we have successfully updated the feed so as to
		// ensure we try repeatedly in case of transient errors e.g. if
		// network is down.
		err = recordFeedUpdate(db, &feed)
		if err != nil {
			log.Printf("Failed to record update on feed [%s]: %s", feed.Name,
				err.Error())
			return err
		}

		feedsUpdated++
	}
	if config.Quiet == 0 {
		log.Printf("Updated %d/%d feed(s).", feedsUpdated, len(feeds))
	}
	return nil
}

// main is the entry point to the program
func main() {
	// we may be given a single argument - a feed name to process. this means
	// we process the single feed only and ignore all others.
	singleFeed := flag.String("feed-name", "",
		"Single feed name to process. Process all feeds if not given.")
	configPath := flag.String("config-file", "",
		"Path to a configuration file.")
	forceUpdate := flag.Bool("force-update", false, "Force updates by ignoring the last update time on feeds.")
	flag.Parse()

	if len(*configPath) == 0 {
		log.Print("You must specify a configuration file.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// load up our settings.
	var settings GorsePollConfig
	err := config.GetConfig(*configPath, &settings)
	if err != nil {
		log.Fatalf("Failed to retrieve config: %s", err.Error())
	}

	// set up the standard logger. we want to set flags to make it give
	// more information.
	log.SetFlags(log.Ltime)

	// connect to the database.
	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s connect_timeout=10",
		settings.DbUser, settings.DbPass, settings.DbName, settings.DbHost)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Failed to connect to the database: " + err.Error())
	}
	defer db.Close()

	// set gorselib settings.
	gorselib.SetQuiet(settings.Quiet != 0)

	// Retrieve our feeds from the database.
	feeds, err := gorselib.RetrieveFeeds(db)
	if err != nil {
		log.Fatal("Failed to retrieve feeds: " + err.Error())
	}

	// we limit ourselves to the one feed. find it and make a new slice with
	// only this feed in it.
	if len(*singleFeed) > 0 {
		feedsSingle := make([]gorselib.RssFeed, 0)
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

	// process & update our feeds.
	err = processFeeds(&settings, db, feeds, *forceUpdate)
	if err != nil {
		log.Fatal("Failed to process feed(s)")
	}
}
