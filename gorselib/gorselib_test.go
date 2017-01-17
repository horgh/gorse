package gorselib

import (
	"fmt"
	"testing"
)

func TestParseAsAtom(t *testing.T) {
	tests := []struct {
		input   string
		output  *Channel
		success bool
	}{
		{
			`<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">

 <title>Test one two</title>
 <link href="http://www.example.com/atom.xml" rel="self"/>
 <link href="http://www.example.com"/>
 <updated>2017-01-11T20:30:23-05:00</updated>
 <id>http://www.example.com-id</id>
 <author>
   <name>John Q. Public</name>
   <email>john@example.com</email>
 </author>

 <entry>
   <title>Test title 1</title>
   <link href="http://www.example.com/test-entry-1"/>
   <updated>2017-01-11T00:00:00-05:00</updated>
   <id>http://www.example.com/test-entry-1-id</id>
   <content type="html">&lt;p&gt;Testing content 1&lt;/p&gt;</content>
</entry>

 <entry>
   <title>Test title 2</title>
   <link href="http://www.example.com/test-entry-2"/>
   <updated>2017-01-12T00:00:00-05:00</updated>
   <id>http://www.example.com/test-entry-2-id</id>
   <content type="html">&lt;p&gt;Testing content 2&lt;/p&gt;</content>
</entry>
</feed>
`,
			&Channel{
				Title:         "Test one two",
				Link:          "http://www.example.com/atom.xml",
				Description:   "",
				PubDate:       "2017-01-11T20:30:23-05:00",
				LastBuildDate: "",
				Items: []Item{
					Item{
						Title:       "Test title 1",
						Link:        "http://www.example.com/test-entry-1",
						Description: "<p>Testing content 1</p>",
						PubDate:     "2017-01-11T00:00:00-05:00",
						GUID:        "http://www.example.com/test-entry-1-id",
					},
					Item{
						Title:       "Test title 2",
						Link:        "http://www.example.com/test-entry-2",
						Description: "<p>Testing content 2</p>",
						PubDate:     "2017-01-12T00:00:00-05:00",
						GUID:        "http://www.example.com/test-entry-2-id",
					},
				},
			},
			true,
		},
	}

	for _, test := range tests {
		ch, err := parseAsAtom([]byte(test.input))
		if err != nil {
			if !test.success {
				continue
			}

			t.Errorf("parseAsAtom(%s) = error %s, wanted success", test.input, err)
			continue
		}

		if !test.success {
			t.Errorf("parseAsAtom(%s) = success, wanted error", test.input)
			continue
		}

		err = channelEqual(ch, test.output)
		if err != nil {
			t.Errorf("parseAsAtom(%s): %s", test.input, err)
			t.Errorf("Got:    %#v", ch)
			t.Errorf("Wanted: %#v", test.output)
			continue
		}
	}
}

func channelEqual(a, b *Channel) error {
	if a.Title != b.Title {
		return fmt.Errorf("channel title mismatch")
	}

	if a.Link != b.Link {
		return fmt.Errorf("channel link mismatch")
	}

	if a.Description != b.Description {
		return fmt.Errorf("channel description")
	}

	if a.PubDate != b.PubDate {
		return fmt.Errorf("channel pubdate mismatch")
	}

	if a.LastBuildDate != b.LastBuildDate {
		return fmt.Errorf("channel lastbuilddate mismatch")
	}

	if len(a.Items) != len(b.Items) {
		return fmt.Errorf("channel items count mismatch")
	}

	for i := range a.Items {
		ai := a.Items[i]
		bi := b.Items[i]

		if ai.Title != bi.Title {
			return fmt.Errorf("item %d title mismatch", i)
		}

		if ai.Link != bi.Link {
			return fmt.Errorf("item %d link mismatch", i)
		}

		if ai.Description != bi.Description {
			return fmt.Errorf("item %d description mismatch", i)
		}

		if ai.PubDate != bi.PubDate {
			return fmt.Errorf("item %d pubdate mismatch", i)
		}

		if ai.GUID != bi.GUID {
			return fmt.Errorf("item %d guid mismatch", i)
		}
	}

	return nil
}
