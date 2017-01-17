package gorselib

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// Channel contains a feed parsed from any format.
type Channel struct {
	Title         string
	Link          string
	Description   string
	PubDate       string
	LastBuildDate string
	Items         []Item
}

// Item contains an item/entry in a feed parsed from any format.
type Item struct {
	Title       string
	Link        string
	Description string
	PubDate     string
	GUID        string
}

// RSSXML is used for parsing RSS.
type RSSXML struct {
	// If xml.Name is specified and has a tag name, we must have this element as
	// the root. I don't do this though because it is case sensitive. Instead,
	// inspect XMLName manually afterwards.
	XMLName xml.Name
	Channel ChannelXML `xml:"channel"`
	Version string     `xml:"version,attr"`
}

// ChannelXML is used for parsing RSS.
type ChannelXML struct {
	XMLName       xml.Name  `xml:"channel"`
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	PubDate       string    `xml:"pubDate"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []ItemXML `xml:"item"`
}

// ItemXML is used for parsing RSS.
type ItemXML struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
	GUID        string   `xml:"guid"`
}

// RDFXML is used for parsing RDF.
type RDFXML struct {
	XMLName xml.Name
	Channel ChannelXML `xml:"channel"`
	Version string     `xml:"version,attr"`

	// For RDF we'll have <item> elements directly as children.
	RDFItems []RDFItemXML `xml:"item"`
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

	ch := &Channel{
		Title:         rssXML.Channel.Title,
		Link:          rssXML.Channel.Link,
		Description:   rssXML.Channel.Description,
		PubDate:       rssXML.Channel.PubDate,
		LastBuildDate: rssXML.Channel.LastBuildDate,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RSS [%s]", ch.Title)
	}

	// TODO: Should we report if there are as an error?

	for _, item := range rssXML.Channel.Items {
		ch.Items = append(ch.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     item.PubDate,
				GUID:        item.GUID,
			})
	}

	return ch, nil
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

	ch := &Channel{
		Title:         rdfXML.Channel.Title,
		Link:          rdfXML.Channel.Link,
		Description:   rdfXML.Channel.Description,
		PubDate:       rdfXML.Channel.PubDate,
		LastBuildDate: rdfXML.Channel.LastBuildDate,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RDF [%s]", ch.Title)
	}

	for _, item := range rdfXML.RDFItems {
		ch.Items = append(ch.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     item.PubDate,
				GUID:        item.GUID,
			})
	}

	return ch, nil
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

	ch := &Channel{
		Title:   atomXML.Title,
		Link:    link,
		PubDate: atomXML.Updated,
	}

	if !config.Quiet {
		log.Printf("Parsed channel as Atom [%s]", ch.Title)
	}

	for _, item := range atomXML.Items {
		link := ""
		// Take the first. Probably we can be more intelligent.
		if len(item.Links) > 0 {
			link = item.Links[0].Href
		}

		ch.Items = append(ch.Items,
			Item{
				Title:       item.Title,
				Link:        link,
				Description: item.Content,
				PubDate:     item.Updated,
				GUID:        item.ID,
			})
	}

	return ch, nil
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
