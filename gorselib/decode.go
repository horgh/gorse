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

// Feed contains a feed parsed from any format.
type Feed struct {
	Title       string
	Link        string
	Description string
	PubDate     time.Time
	Items       []Item
}

// Item contains an item/entry in a feed parsed from any format.
type Item struct {
	Title       string
	Link        string
	Description string
	PubDate     time.Time
}

// rssXML is used for parsing/encoding RSS.
type rssXML struct {
	// If xml.Name is specified and has a tag name, we must have this element as
	// the root. I don't do this though because it is case sensitive. Instead,
	// inspect XMLName manually afterwards.
	XMLName xml.Name
	Channel rssChannelXML `xml:"channel"`
	Version string        `xml:"version,attr"`
}

// rssChannelXML is used for parsing/encoding RSS.
type rssChannelXML struct {
	XMLName     xml.Name     `xml:"channel"`
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	Description string       `xml:"description"`
	PubDate     string       `xml:"pubDate"`
	Items       []rssItemXML `xml:"item"`
}

// rssItemXML is used for parsing/encoding RSS.
type rssItemXML struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"pubDate"`
}

// rdfXML is used for parsing RDF.
type rdfXML struct {
	// Element name. Don't specify here so we can check case insensitively.
	XMLName xml.Name

	Channel rdfChannelXML `xml:"channel"`

	RDFItems []rdfItemXML `xml:"item"`
}

// rdfChannelXML is part of parsing RDF.
type rdfChannelXML struct {
	XMLName     xml.Name `xml:"channel"`
	Title       string   `xml:"title"`
	Links       []string `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"date"`
}

// rdfItemXML is used for parsing <rdf> item XML.
type rdfItemXML struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	PubDate     string   `xml:"date"`
}

// atomXML describes an Atom feed. We use it for parsing. See
// https://tools.ietf.org/html/rfc4287
type atomXML struct {
	// The element name. Enforce it is atom:feed
	XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`

	// Title is human readable. It must be present.
	Title string `xml:"title"`

	// Web resource. Zero or more. Feeds should contain with with rel=self.
	Links []atomLink `xml:"link"`

	// Last time feed was updated.
	Updated string `xml:"updated"`

	Items []atomItemXML `xml:"entry"`
}

// atomLink describes a <link> element.
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

// atomItemXML describes an item/entry in the feed. Atom calls these entries,
// but for consistency with other formats I support, I call them items.
type atomItemXML struct {
	// Human readable title. Must be present.
	Title string `xml:"title"`

	// Web resource. Zero or more.
	Links []atomLink `xml:"link"`

	// Last time entry updated. Must be present.
	Updated string `xml:"updated"`

	// Content is optional.
	Content string `xml:"content"`
}

// ParseFeedXML takes the raw XML and returns a struct describing the feed.
//
// We support various formats. Try our best to decode the feed.
func ParseFeedXML(data []byte) (*Feed, error) {
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
func parseAsRSS(data []byte) (*Feed, error) {
	// Decode from XML.

	// To see how Unmarshal() works, refer to the documentation. Basically we
	// have to tag the struct fields in the special format as in the package
	// structs.
	rssXML := rssXML{}

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

	feed := &Feed{
		Title:       rssXML.Channel.Title,
		Link:        rssXML.Channel.Link,
		Description: rssXML.Channel.Description,
		PubDate:     parseTime(rssXML.Channel.PubDate),
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RSS [%s]", feed.Title)
	}

	for _, item := range rssXML.Channel.Items {
		feed.Items = append(feed.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     parseTime(item.PubDate),
			})
	}

	return feed, nil
}

// parseAsRDF attempts to parse the buffer as if it contains an RDF feed.
//
// See parseAsRSS() for a similar function, but for RSS.
func parseAsRDF(data []byte) (*Feed, error) {
	rdfXML := rdfXML{}

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

	link := ""
	if len(rdfXML.Channel.Links) > 0 {
		link = rdfXML.Channel.Links[0]
	}

	feed := &Feed{
		Title:       rdfXML.Channel.Title,
		Link:        link,
		Description: rdfXML.Channel.Description,
		PubDate:     parseTime(rdfXML.Channel.PubDate),
	}

	if !config.Quiet {
		log.Printf("Parsed channel as RDF [%s]", feed.Title)
	}

	for _, item := range rdfXML.RDFItems {
		feed.Items = append(feed.Items,
			Item{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     parseTime(item.PubDate),
			})
	}

	return feed, nil
}

// parseAsAtom attempts to parse the buffer as Atom.
//
// See parseAsRSS() and parseAsRDF() for similar parsing. Also I omit comments
// that would be repeated here if they are in those functions.
func parseAsAtom(data []byte) (*Feed, error) {
	atomXML := atomXML{}

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

	feed := &Feed{
		Title:   atomXML.Title,
		Link:    link,
		PubDate: parseTime(atomXML.Updated),
	}

	if !config.Quiet {
		log.Printf("Parsed channel as Atom [%s]", feed.Title)
	}

	for _, item := range atomXML.Items {
		link := ""
		// Take the first. Probably we can be more intelligent.
		if len(item.Links) > 0 {
			link = item.Links[0].Href
		}

		feed.Items = append(feed.Items, Item{
			Title:       item.Title,
			Link:        link,
			Description: item.Content,
			PubDate:     parseTime(item.Updated),
		})
	}

	return feed, nil
}

// parseTime tries to retrieve a publication date for the item.
//
// We try parsing using multiple formats, and fall back to a default of the
// current time if none succeed.
func parseTime(pubDate string) time.Time {
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
