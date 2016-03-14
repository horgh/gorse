#
# makefile for packaging.
#

prefix=/usr/sbin
bindir=$(prefix)
WWWDIR=/usr/share/gorse/www

all: gorse

install:
	@install -D -m 0755 gorse $(DESTDIR)$(bindir)/gorse
	@install -D -m 0644 gorse.init $(DESTDIR)/etc/init.d/gorse
	@install -D -m 0755 gorse.config.example $(DESTDIR)/etc/gorse.conf
	@install -D -m 0644 static/gorse.css $(DESTDIR)/$(WWWDIR)/static/gorse.css
	@install -D -m 0644 static/gorse.js $(DESTDIR)/$(WWWDIR)/static/gorse.js
	@install -D -m 0644 static/jquery-2.1.3.min.js \
		$(DESTDIR)/$(WWWDIR)/static/jquery-2.1.3.min.js
	@install -D -m 0644 templates/_footer.html \
		$(DESTDIR)/$(WWWDIR)/templates/_footer.html
	@install -D -m 0644 templates/_header.html \
		$(DESTDIR)/$(WWWDIR)/templates/_header.html
	@install -D -m 0644 templates/_list_items.html \
		$(DESTDIR)/$(WWWDIR)/templates/_list_items.html
