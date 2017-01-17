package gorselib

import (
	"encoding/xml"
	"io/ioutil"
	"log"
	"time"
)

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
	rss := rssXML{
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
		itemXML := rssItemXML{
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
