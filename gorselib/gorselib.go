//
// Package gorselib provides helper function for interacting with RSS feeds.
// primarily this surrounds building and reading/parsing.
//
package gorselib

import (
	"bytes"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/ioutil"
	"log"
	"regexp"
	"strings"
	"time"

	"code.google.com/p/go-charset/charset"
	// go-charset: for charset conversion
	_ "code.google.com/p/go-charset/data"
)

// Config controls package wide settings.
type Config struct {
	// control whether we have verbose output (or not)
	Quiet bool
}

// use a global default set of settings.
// see package log for a similar approach (global default settings)
var config = Config{
	Quiet: false,
}

// RSSFeed describes a feed.
type RSSFeed struct {
	// database id of the feed.
	ID int64

	// name of the feed.
	Name string

	// description of the fed.
	Description string

	// uri to the feed.
	URI string

	// update frequency in seconds.
	UpdateFrequencySeconds int64

	// last time we updated.
	LastUpdateTime time.Time

	Items []RSSItem
}

// RSSItem describes an item in a feed.
type RSSItem struct {
	FeedName    string
	ID          int64
	Title       string
	Description string
	// HTML version of description.
	// TODO this is only used in gorse...
	DescriptionHTML       template.HTML
	URI                   string
	PublicationDate       time.Time
	PublicationDateString string
}

// Item is abstracted from ItemXML and RDFItemXML.
// Why? Because I will convert ItemXML and RDFItemXML both into this form.
type Item struct {
	Title       string
	Link        string
	Description string
	PubDate     string
	GUID        string
}

// Channel is abstracted from ChannelXML.
// Why? Because I will convert ItemXML and RDFItemXML both into Item and need
// to then store it here.
type Channel struct {
	Title         string
	Link          string
	Description   string
	PubDate       string
	LastBuildDate string
	Items         []Item
}

// ItemXML and the other *XML structs are for parsing the XML. We fill them up
// with data we parse out. However, they are also specially set up for the
// xml.Unmarshal() function (i.e., the tags on each field)
type ItemXML struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
	GUID        string   `xml:"guid"`
}

// ChannelXML is used for parsing XML.
type ChannelXML struct {
	XMLName       xml.Name  `xml:"channel"`
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	PubDate       string    `xml:"pubDate"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []ItemXML `xml:"item"`
}

// RDFItemXML is used for parsing <rdf> item XML.
type RDFItemXML struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"date"`
	// TODO: we don't need Guid? rdf does not have it
	GUID string `xml:"guid"`
}

// RSSXML is used for parsing RSS XML.
type RSSXML struct {
	// If xml.Name is specified and has a tag name, we must have this element as
	// the root. I don't do this though because it is case sensitive. Instead,
	// inspect XMLName manually afterwards.
	XMLName xml.Name
	Channel ChannelXML `xml:"channel"`
	Version string     `xml:"version,attr"`
}

// RDFXML is used for parsing RDF XML.
type RDFXML struct {
	XMLName xml.Name
	Channel ChannelXML `xml:"channel"`
	Version string     `xml:"version,attr"`

	// For RDF we'll have <item> elements directly as children.
	RDFItems []RDFItemXML `xml:"item"`
}

// SetQuiet controls the gorselib setting 'Quiet'.
func SetQuiet(quiet bool) {
	config.Quiet = quiet
}

// SanitiseItemText takes text (e.g., title or description) and removes
// any html markup. this is because some feeds (e.g., slashdot) include
// a lot of markup I don't want to actually show.
//
// we also un-decode html entities since apparently we can get these through
// to this point (they will be encoded again as necessary when we render the
// page). for example in a raw xml from slashdot we have this:
// <item><title>AT&amp;amp;T Gets Patent To Monitor and Track File-Sharing Traffic</title>
// which gets placed into the database as:
// AT&amp;T Gets Patent To Monitor and Track File-Sharing Traffic
//
// this can be used to take any string which has html in it to clean
// up that string and make it non-html.
//
// NOTE: while elements such as 'title' can have html in them, this seems
//   applied inconsistently. for instance, consider this title from
//   a slashdot feed:
//   <title>Google Maps Updated With Skyfall&lt;/em&gt; Island Japan Terrain</title>
//   that is: </em> in there but no <em>.
//   in the database this is present as </em>.
//
//   thus we do not place the html into the page raw.
func SanitiseItemText(text string) (string, error) {
	// first remove raw html.
	re, err := regexp.Compile("(?s)<.*?>")
	if err != nil {
		log.Printf("Failed to compile html regexp: %s", err.Error())
		return text, err
	}
	text = re.ReplaceAllString(text, "")

	// then undecode html entities.
	text = html.UnescapeString(text)

	// turn any multiple spaces into a single space.
	re, err = regexp.Compile("(?s)\\s+")
	if err != nil {
		log.Printf("Failed to compile whitespace regexp: %s", err.Error())
		return text, err
	}
	text = re.ReplaceAllString(text, " ")

	return text, nil
}

// GetItemPubDate tries to retrieve a publication date for the item.
// we try parsing using multiple formats, and fall back to a default
// of the current time if none succeed.
func GetItemPubDate(pubDate string) time.Time {
	if len(pubDate) == 0 {
		if !config.Quiet {
			log.Print("No pub date given - using default.")
		}
		return time.Now()
	}

	// use RFC1123 time format for parsing. this appears to be what
	// is present in the slashdot feed, though I expect this could
	// vary in other feed sources...
	// slashdot's feed:
	// Sat, 29 Jun 2013 18:20:00 GMT
	pubDateTimeParsed, err := time.Parse(time.RFC1123, pubDate)
	// we use the parsed time only if we had no errors parsing it.
	if err == nil {
		return pubDateTimeParsed
	}

	// try another format.
	// torrentfreak rss feed format:
	// Sun, 30 Jun 2013 21:26:26 +0000
	// Mon, 10 Jun 2013 21:04:57 +0000
	pubDateTimeParsed, err = time.Parse(time.RFC1123Z, pubDate)
	// we use the parsed time only if we had no errors parsing it.
	if err == nil {
		return pubDateTimeParsed
	}

	// slashdot rdf format.
	// 2015-03-03T21:29:00+00:00
	// NOTE: RFC3339 is not exactly this it seems?
	pubDateTimeParsed, err = time.Parse(time.RFC3339, pubDate)
	if err == nil {
		return pubDateTimeParsed
	}

	if !config.Quiet {
		log.Printf("No format worked for date [%s] - using default - NOW", pubDate)
	}
	return time.Now()
}

// ParseFeedXML takes the raw xml and returns a struct describing the
// feed data.
func ParseFeedXML(data []byte) (*Channel, error) {
	// It is possible for us to not have valid XML. In such a case, the xml
	// Decode function will not always complain. One way for this to happen is if
	// you do not specify what tag the XML must start with.
	err := looksLikeXML(data)
	if err != nil {
		return nil, err
	}

	channelRSS, errRSS := parseAsRSS(data)
	if errRSS == nil {
		return channelRSS, nil
	}

	channelRDF, errRDF := parseAsRDF(data)
	if errRDF == nil {
		return channelRDF, nil
	}

	return nil, fmt.Errorf("Unable to parse as RSS (%v) or RDF (%v)", errRSS,
		errRDF)
}

// looksLikeXML applies some simple checks that we have an XML document.
func looksLikeXML(data []byte) error {
	prefix := `<?xml version="1.0" encoding="`

	if len(data) < len(prefix) {
		return errors.New("Buffer is too short to have XML header.")
	}

	for i := 0; i < len(prefix); i++ {
		if data[i] != prefix[i] {
			return errors.New("Buffer does not have XML header.")
		}
	}

	return nil
}

// parseAsRSS attempts to parse the buffer as if it contains an RSS feed.
func parseAsRSS(data []byte) (*Channel, error) {
	// Decode from XML.

	// To see how Unmarshal() works, refer to the documentation.
	// Basically we have to tag the struct fields in the special format as in the
	// package structs.
	rssXML := RSSXML{}

	// We can use xml.Unmarshal() except in cases where we need to convert between
	// charsets. Which we want to be able to do, so we do not use Unmarshal().
	// For example if we have:
	// <?xml version="1.0" encoding="ISO-8859-1"?>
	// Then we have to create an xml.Decoder and provide it a CharsetReader
	// function.
	// see http://stackoverflow.com/questions/6002619/unmarshal-an-iso-8859-1-xml-input-in-go

	// Decoder wants an io.Reader.
	byteReader := bytes.NewBuffer(data)

	decoder := xml.NewDecoder(byteReader)

	decoder.CharsetReader = charset.NewReader

	err := decoder.Decode(&rssXML)
	if err != nil {
		return nil, fmt.Errorf("RSS XML decode error: %v", err)
	}

	if strings.ToLower(rssXML.XMLName.Local) != "rss" {
		return nil, errors.New("Base tag is not RSS.")
	}

	// Build a channel struct now. It's common to the base formats we support.

	channel := Channel{
		Title:         rssXML.Channel.Title,
		Link:          rssXML.Channel.Link,
		Description:   rssXML.Channel.Description,
		PubDate:       rssXML.Channel.PubDate,
		LastBuildDate: rssXML.Channel.LastBuildDate,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RSS [%s]", channel.Title)
	}

	// TODO: Should we report if there are no items at all as an error?

	for _, item := range rssXML.Channel.Items {
		channel.Items = append(channel.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     item.PubDate,
				GUID:        item.GUID,
			})
	}

	return &channel, nil
}

// parseAsRDF attempts to parse the buffer as if it contains an RDF feed.
//
// See parseAsRSS().
func parseAsRDF(data []byte) (*Channel, error) {
	rdfXML := RDFXML{}

	byteReader := bytes.NewBuffer(data)
	decoder := xml.NewDecoder(byteReader)
	decoder.CharsetReader = charset.NewReader

	err := decoder.Decode(&rdfXML)
	if err != nil {
		return nil, fmt.Errorf("RDF XML decode error: %v", err)
	}

	if strings.ToLower(rdfXML.XMLName.Local) != "rdf" {
		return nil, errors.New("Base tag is not RDF.")
	}

	// TODO: Does RDF have all of these fields?

	channel := Channel{
		Title:         rdfXML.Channel.Title,
		Link:          rdfXML.Channel.Link,
		Description:   rdfXML.Channel.Description,
		PubDate:       rdfXML.Channel.PubDate,
		LastBuildDate: rdfXML.Channel.LastBuildDate,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RDF [%s]", channel.Title)
	}

	for _, item := range rdfXML.RDFItems {
		channel.Items = append(channel.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     item.PubDate,
				GUID:        item.GUID,
			})
	}

	return &channel, nil
}

// WriteFeedXML takes an RSSFeed and generates and writes an XML file.
// this generates RSS 2.0.1.
// see http://www.rssboard.org/rss-specification
// validate the output files using:
// http://www.rssboard.org/rss-validator
// overall the XML structure is:
// <rss><channel><item></item><item></item>...</channel></rss>
//
// NOTE: a note on timestamps. the rss spec says we should use
//   RFC 822, but the time.RFC1123Z format looks closest to their
//   examples, so I use that.
func WriteFeedXML(feed *RSSFeed, filename string) error {
	// top level element. version is required. we use 2.0 even though we
	// are generating 2.0.1 as that, it seems, is the spec.
	rss := RSSXML{
		Version: "2.0",
	}
	rss.XMLName.Local = "rss"

	// set up the channel metadata.
	// <channel/>
	//   <title/> Channel title
	//   <link/> URL corresponding to channel
	//   <description/> Phrase describing the channel
	//   not required, but nice:
	//   <pubDate/> Publication date for the content
	//   <lastBuildDate/> Last time content of channel changed
	rss.Channel.Title = feed.Name
	rss.Channel.Link = feed.URI
	rss.Channel.Description = feed.Description
	// XXX: technically these dates maybe should be different...
	rss.Channel.PubDate = feed.LastUpdateTime.Format(time.RFC1123Z)
	rss.Channel.LastBuildDate = rss.Channel.PubDate

	// set up each of our items.
	//   <item/>
	//     <title/> Title of the item
	//     <link/> URL of the item
	//     <description/> Item synopsis
	//     <pubDate/> When the item was published
	//     <guid/> Arbitrary string unique to the item
	for _, item := range feed.Items {
		itemXML := ItemXML{
			Title:       item.Title,
			Link:        item.URI,
			Description: item.Description,
			PubDate:     item.PublicationDate.Format(time.RFC1123Z),
			// use the URI as guid - it should be uniquely identifying
			// the post after all. NOTE: the guid has no required format
			// other than it is intended to be unique.
			GUID: item.URI,
		}
		rss.Channel.Items = append(rss.Channel.Items, itemXML)
	}

	// build the xml data.
	xmlBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal xml: %s", err.Error())
		return err
	}

	// add the xml header <?xml .. ?>
	xmlHeader := []byte(xml.Header)
	var xmlDoc []byte
	for _, v := range xmlHeader {
		xmlDoc = append(xmlDoc, v)
	}
	for _, v := range xmlBody {
		xmlDoc = append(xmlDoc, v)
	}

	// write our file.
	err = ioutil.WriteFile(filename, xmlDoc, 0644)
	if err != nil {
		log.Printf("Failed to write file [%s]: %s", filename, err.Error())
		return err
	}

	if !config.Quiet {
		log.Printf("Wrote file [%s]", filename)
	}
	return nil
}

// RetrieveFeeds finds rss feeds from the database.
func RetrieveFeeds(db *sql.DB) ([]RSSFeed, error) {
	// retrieve the feeds from the database.
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
	// build our slice of feeds.
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
	return feeds, nil
}

// GetDurationUntilNextUpdateForDisplay builds a string describing how
// long until the next update.
// for example, '2 hours'
func (feed RSSFeed) GetDurationUntilNextUpdateForDisplay() (string, error) {
	// we want to know how long since our last update as we will compare
	// this with how long our update frequency is.
	timeSinceLastUpdate := time.Now().Sub(feed.LastUpdateTime)

	// take our update frequency and subtract the time since our last update
	// to find the duration until the next update.
	nextUpdateTimeSeconds := float64(feed.UpdateFrequencySeconds) -
		timeSinceLastUpdate.Seconds()

	minutes := nextUpdateTimeSeconds / 60
	hours := int(minutes / 60)
	minutesInt := int(minutes)

	// it is possible for us to be past the time where we should have
	// updated (if update script has not run, or there is a problem).
	// just say that we should be updating now.
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

// GetDurationSinceUpdateForDisplay builds a string describing how long ago
// the last update was.
// for example, '2 hours'.
func (feed RSSFeed) GetDurationSinceUpdateForDisplay() string {
	// now - last update time.
	difference := time.Now().Sub(feed.LastUpdateTime)

	// hours and minutes are given as floating - we want to round them.
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

// GetUpdateFrequencyForDisplay builds a string suitable for the user
// out of our update frequency seconds.
func (feed RSSFeed) GetUpdateFrequencyForDisplay() string {
	updateMinutes := feed.UpdateFrequencySeconds / 60
	updateHours := updateMinutes / 60

	// we give a rough idea of the time rather than being too specific.
	// for example if we have over an hour then we just say there are
	// x hour(s) and say nothing about the minute(s).
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
