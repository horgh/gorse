Gorse is an RSS reader. You provide it with RSS feeds to monitor, and its
poller program, gorsepoll, pulls the contents of the feed into a database.
Gorse itself provides an interface to view and read the feeds.

It can work with feeds in RSS, RDF, and Atom formats.


# Components

## gorse
A web frontend to a database of feeds and their items/entries.


## gorsepoll
This is an RSS poller. It takes feeds to poll from a database, and populates
the database with the items it finds.

It should be run periodically, such as through cron.

It tracks when it last updated a feed, and will not try it again until a period
elapsed. It considers a feed updated when it successfully fetches and parses a
feed.


# Setup
To set up the database:

    createuser -D -E -P -R -S gorse
    createdb -E UTF8 -l en_CA.UTF-8 -O gorse gorse
    cat schema.sql upgrade1.sql feeds.sql > install.sql
    psql < install.sql
