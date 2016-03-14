# Summary

Gorse is a web RSS reader. You provide it with RSS feeds to monitor, and its
backend poller program, gorsepoll, pulls the contents of the feed into the local
database. Gorse itself provides an interface to viewing and reading the contents
of the feeds.

Directories:

  * gorse: A web frontend to a database of feeds/feed items
  * gorsepoll: A utility to retrieve feed items and insert them into a
    database
  * gorselib: Library shared by the above.


# Gorsepoll

This is an RSS poll utility. It takes feeds to poll from a database,
and populates the database with feed items.

Behaviour notes:

  * We record a feed has been updated and will not try it again until
    its update frequency period has elapsed only if we successfully
    fetch it.

Setup:

    createuser -D -E -P -R -S gorse
    createdb -E UTF8 -l en_CA.UTF-8 -O gorse gorse
    cat schema.sql upgrade1.sql feeds.sql > install.sql
    psql < install.sql
