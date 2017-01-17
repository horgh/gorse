package gorselib

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"time"
)

// The input types (rssXML, rssChannelXML, rssItemXML) include less fields
// than I write out. To keep the decoding side from getting overcomplicated
// vs. the encoding side, use different types here.
//
// Differences:
//
// LastBuildDate is not in rssChannelXML
//
// GUID is not in rssItemXML

// <rss version="2.0">
//   <channel> Info about the feed, and its items
type outXML struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	Channel outChannelXML `xml:"channel"`
}

// <channel>
//   <title>         Channel title
//   <link>          URL corresponding to channel
//   <description>   Phrase describing the channel
//   <pubDate>       Publication date for the content
//   <lastBuildDate> Last time content of channel changed
type outChannelXML struct {
	Title         string       `xml:"title"`
	Link          string       `xml:"link"`
	Description   string       `xml:"description"`
	PubDate       string       `xml:"pubDate"`
	LastBuildDate string       `xml:"lastBuildDate"`
	Items         []outItemXML `xml:"item"`
}

// <item>
//   <title>       Title of the item
//   <link>        URL of the item
//   <description> Item synopsis
//   <pubDate>     When the item was published
//   <guid>        Arbitrary string unique to the item
type outItemXML struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
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
func WriteFeedXML(feed Feed, filename string) error {
	xmlDoc, err := makeXML(feed)
	if err != nil {
		return fmt.Errorf("unable to generate XML: %s", err)
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

// Turn the feed into XML.
func makeXML(feed Feed) ([]byte, error) {
	out := outXML{
		// Version is required. We use 2.0 even though we are generating 2.0.1 as
		// that, it seems, is the spec.
		Version: "2.0",
		Channel: outChannelXML{
			Title:       feed.Title,
			Link:        feed.Link,
			Description: feed.Description,
			// TODO: These dates could/should be different.
			PubDate:       feed.PubDate.Format(time.RFC1123Z),
			LastBuildDate: feed.PubDate.Format(time.RFC1123Z),
		},
	}

	for _, item := range feed.Items {
		out.Channel.Items = append(out.Channel.Items, outItemXML{
			Title:       item.Title,
			Link:        item.Link,
			Description: item.Description,
			PubDate:     item.PubDate.Format(time.RFC1123Z),
			// Use the URI as GUID. It should be uniquely identifying the post after
			// all. Note the GUID has no required format other than it is intended to
			// be unique.
			GUID: item.Link,
		})
	}

	// Convert to XML.
	xmlBody, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal xml: %s", err)
	}

	// Put document together.

	var xmlDoc []byte

	// Add the XML header <?xml .. ?>
	xmlHeader := []byte(xml.Header)
	for _, v := range xmlHeader {
		xmlDoc = append(xmlDoc, v)
	}

	for _, v := range xmlBody {
		xmlDoc = append(xmlDoc, v)
	}

	return xmlDoc, nil
}
