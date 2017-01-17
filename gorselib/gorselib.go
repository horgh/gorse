// Package gorselib provides helper function for interacting with RSS, RDF,
// and Atom feeds. Primarily this surrounds building and reading/parsing.
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

	"golang.org/x/net/html/charset"
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

// RSSFeed describes a feed.
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

// RSSItem describes an item in a feed.
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

// Item is abstracted from ItemXML and RDFItemXML. Why? Because I will convert
// ItemXML and RDFItemXML both into this form.
type Item struct {
	Title       string
	Link        string
	Description string
	PubDate     string
	GUID        string
}

// Channel is abstracted from ChannelXML. Why? Because I will convert ItemXML
// and RDFItemXML both into Item and need to then store it here.
type Channel struct {
	Title         string
	Link          string
	Description   string
	PubDate       string
	LastBuildDate string
	Items         []Item
}

// ItemXML and the other *XML structs are for parsing the XML. We fill them
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
	// TODO: We don't need GUID? RDF does not have it
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

// AtomXML describes an Atom feed. We use it for parsing. See
// https://tools.ietf.org/html/rfc4287
type AtomXML struct {
	// The element name. Enforce it is atom:feed
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`

	// Title is human readable. It must be present.
	Title string `xml:"title"`

	// Web resource. Zero or more. Feeds should contain with with rel=self.
	Links []AtomLink `xml:"link"`

	// ID must be present and must be an IRI. Unique but might not be a web
	// resource.
	ID string `xml:"id"`

	// Last time feed was updated.
	Updated string `xml:"updated"`

	Items []AtomItemXML `xml:"entry"`
}

// AtomLink describes a <link> element.
type AtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

// AtomItemXML describes an item/entry in the feed. Atom calls these entries,
// but for consistency with other formats I support, I call them items.
type AtomItemXML struct {
	// Human readable title. Must be present.
	Title string `xml:"title"`

	// Web resource. Zero or more.
	Links []AtomLink `xml:"link"`

	// ID must be present and must be an IRI. Unique but might not be a web
	// resource.
	ID string `xml:"id"`

	// Last time entry updated. Must be present.
	Updated string `xml:"updated"`

	// Content is optional.
	Content string `xml:"content"`
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

// GetItemPubDate tries to retrieve a publication date for the item.
//
// We try parsing using multiple formats, and fall back to a default of the
// current time if none succeed.
func GetItemPubDate(pubDate string) time.Time {
	if len(pubDate) == 0 {
		if !config.Quiet {
			log.Print("No pub date given - using default.")
		}
		return time.Now()
	}

	// Use RFC1123 time format for parsing. This appears to be what is present in
	// the Slashdot feed, though I expect this could vary in other feed
	// sources...
	//
	// Slashdot's feed: Sat, 29 Jun 2013 18:20:00 GMT
	pubDateTimeParsed, err := time.Parse(time.RFC1123, pubDate)
	// We use the parsed time only if we had no errors parsing it.
	if err == nil {
		return pubDateTimeParsed
	}

	// Try another format.
	//
	// Torrentfreak RSS feed format:
	//
	// Sun, 30 Jun 2013 21:26:26 +0000
	//
	// Mon, 10 Jun 2013 21:04:57 +0000
	pubDateTimeParsed, err = time.Parse(time.RFC1123Z, pubDate)
	// We use the parsed time only if we had no errors parsing it.
	if err == nil {
		return pubDateTimeParsed
	}

	// Slashdot RDF format: 2015-03-03T21:29:00+00:00
	//
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

// ParseFeedXML takes the raw XML and returns a struct describing the feed.
//
// We support various formats. Try our best to decode the feed.
func ParseFeedXML(data []byte) (*Channel, error) {
	// It is possible for us to not have valid XML. In such a case, the XML
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

	channelAtom, errAtom := parseAsAtom(data)
	if errAtom == nil {
		return channelAtom, nil
	}

	return nil, fmt.Errorf("unable to parse as RSS, RDF, or Atom")
}

// looksLikeXML applies some simple checks to know if we have an XML document.
func looksLikeXML(data []byte) error {
	prefix := `<?xml version="1.0" encoding="`

	if len(data) < len(prefix) {
		return errors.New("buffer is too short to have XML header")
	}

	for i := 0; i < len(prefix); i++ {
		if data[i] != prefix[i] {
			return errors.New("buffer does not have XML header")
		}
	}

	return nil
}

// parseAsRSS attempts to parse the buffer as if it contains an RSS feed.
func parseAsRSS(data []byte) (*Channel, error) {
	// Decode from XML.

	// To see how Unmarshal() works, refer to the documentation. Basically we
	// have to tag the struct fields in the special format as in the package
	// structs.
	rssXML := RSSXML{}

	// We can use xml.Unmarshal() except in cases where we need to convert between
	// charsets. Which we want to be able to do, so we do not use Unmarshal().
	//
	// For example if we have:
	// <?xml version="1.0" encoding="ISO-8859-1"?>
	//
	// Then we have to create an xml.Decoder and provide it a CharsetReader
	// function. See
	// http://stackoverflow.com/questions/6002619/unmarshal-an-iso-8859-1-xml-input-in-go

	// Decoder wants an io.Reader.
	byteReader := bytes.NewBuffer(data)

	decoder := xml.NewDecoder(byteReader)

	decoder.CharsetReader = charset.NewReaderLabel

	err := decoder.Decode(&rssXML)
	if err != nil {
		return nil, fmt.Errorf("RSS XML decode error: %v", err)
	}

	if strings.ToLower(rssXML.XMLName.Local) != "rss" {
		return nil, errors.New("base tag is not RSS")
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

	// TODO: Should we report if there are as an error?

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
// See parseAsRSS() for a similar function, but for RSS.
func parseAsRDF(data []byte) (*Channel, error) {
	rdfXML := RDFXML{}

	byteReader := bytes.NewBuffer(data)
	decoder := xml.NewDecoder(byteReader)
	decoder.CharsetReader = charset.NewReaderLabel

	err := decoder.Decode(&rdfXML)
	if err != nil {
		return nil, fmt.Errorf("RDF XML decode error: %v", err)
	}

	if strings.ToLower(rdfXML.XMLName.Local) != "rdf" {
		return nil, errors.New("base tag is not RDF")
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

// parseAsAtom attempts to parse the buffer as Atom.
//
// See parseAsRSS() and parseAsRDF() for similar parsing. Also I omit comments
// that would be repeated here if they are in those functions.
func parseAsAtom(data []byte) (*Channel, error) {
	atomXML := AtomXML{}

	byteReader := bytes.NewBuffer(data)
	decoder := xml.NewDecoder(byteReader)
	decoder.CharsetReader = charset.NewReaderLabel

	err := decoder.Decode(&atomXML)
	if err != nil {
		return nil, fmt.Errorf("Atom XML decode error: %v", err)
	}

	// May have multiple <link> elements. Look for rel=self.
	link := ""
	for _, l := range atomXML.Links {
		if l.Rel != "self" {
			continue
		}
		link = l.Href
		break
	}

	channel := &Channel{
		Title:   atomXML.Title,
		Link:    link,
		PubDate: atomXML.Updated,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as Atom [%s]", channel.Title)
	}

	for _, item := range atomXML.Items {
		link := ""
		// Take the first. Probably we can be more intelligent.
		if len(item.Links) > 0 {
			link = item.Links[0].Href
		}

		channel.Items = append(channel.Items,
			Item{
				Title:       item.Title,
				Link:        link,
				Description: item.Content,
				PubDate:     item.Updated,
				GUID:        item.ID,
			})
	}

	return channel, nil
}

// WriteFeedXML takes an RSSFeed and generates and writes an XML file.
//
// This function generates RSS 2.0.1.
//
// See http://www.rssboard.org/rss-specification
//
// You can validate the output files using:
// http://www.rssboard.org/rss-validator
//
// Overall the XML structure is:
// <rss><channel><item></item><item></item>...</channel></rss>
//
// A note on timestamps: The RSS spec says we should use RFC 822, but the
// time.RFC1123Z format looks closest to their examples, so I use that.
func WriteFeedXML(feed *RSSFeed, filename string) error {
	// Top level element. Version is required. We use 2.0 even though we are
	// generating 2.0.1 as that, it seems, is the spec.
	rss := RSSXML{
		Version: "2.0",
	}
	rss.XMLName.Local = "rss"

	// Set up the channel metadata.
	// <channel/>
	//   <title/> Channel title
	//   <link/> URL corresponding to channel
	//   <description/> Phrase describing the channel
	//   Not required, but nice to have:
	//   <pubDate/> Publication date for the content
	//   <lastBuildDate/> Last time content of channel changed
	rss.Channel.Title = feed.Name
	rss.Channel.Link = feed.URI
	rss.Channel.Description = feed.Description
	// TODO: Technically these dates maybe should be different...
	rss.Channel.PubDate = feed.LastUpdateTime.Format(time.RFC1123Z)
	rss.Channel.LastBuildDate = rss.Channel.PubDate

	// Set up each of our items.
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
			// Use the URI as GUID. It should be uniquely identifying the post after
			// all. Note the GUID has no required format other than it is intended to
			// be unique.
			GUID: item.URI,
		}

		rss.Channel.Items = append(rss.Channel.Items, itemXML)
	}

	// Build the XML.
	xmlBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal xml: %s", err)
		return err
	}

	// Add the XML header <?xml .. ?>
	xmlHeader := []byte(xml.Header)
	var xmlDoc []byte
	for _, v := range xmlHeader {
		xmlDoc = append(xmlDoc, v)
	}
	for _, v := range xmlBody {
		xmlDoc = append(xmlDoc, v)
	}

	err = ioutil.WriteFile(filename, xmlDoc, 0644)
	if err != nil {
		log.Printf("Failed to write file [%s]: %s", filename, err)
		return err
	}

	if !config.Quiet {
		log.Printf("Wrote file [%s]", filename)
	}
	return nil
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
