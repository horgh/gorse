CREATE FUNCTION trigger_lowercase_email()
RETURNS TRIGGER
AS $$
BEGIN
  NEW.email = LOWER(email);
  return NEW;
END
$$
LANGUAGE plpgsql;

CREATE FUNCTION trigger_set_update_time()
RETURNS TRIGGER
AS $$
BEGIN
  NEW.update_time = NOW();
  return NEW;
END
$$
LANGUAGE plpgsql;

-- Track each feed to work with.
CREATE TABLE rss_feed (
	id                       SERIAL,
	name                     VARCHAR NOT NULL,
	uri                      VARCHAR NOT NULL,
	update_frequency_seconds INTEGER NOT NULL,

	-- Whether the poller actually polls this.
	active                   BOOLEAN NOT NULL DEFAULT true,

	last_update_time         TIMESTAMP WITH TIME ZONE,
  create_time              TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time              TIMESTAMP WITH TIME ZONE,
  last_payload             BYTEA,

  -- Whether new items go directly to read state.
  archive                  BOOLEAN NOT NULL,

	UNIQUE (name),
	UNIQUE (uri),
	PRIMARY KEY (id)
);

CREATE INDEX ON rss_feed (active);

CREATE TRIGGER biu_rss_feed
BEFORE UPDATE ON rss_feed
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

-- Track RSS feed items.
CREATE TABLE rss_item (
	id               SERIAL,
	-- HTML encoded.
	title            VARCHAR NOT NULL,
	-- HTML encoded.
	description      VARCHAR NOT NULL,
	-- HTML encoded.
	link             VARCHAR NOT NULL,
	rss_feed_id      INTEGER NOT NULL REFERENCES rss_feed(id)
		               ON UPDATE CASCADE ON DELETE CASCADE,
	publication_date TIMESTAMP WITH TIME ZONE NOT NULL,
  create_time      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time      TIMESTAMP WITH TIME ZONE,
  guid             VARCHAR,

	-- It is possible to have same title/description I suppose.
	UNIQUE (rss_feed_id, link),
	UNIQUE (rss_feed_id, guid),
	PRIMARY KEY (id)
);

CREATE TRIGGER biu_rss_item
BEFORE UPDATE ON rss_item
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

-- A user. Each user subscribes to feeds.
CREATE TABLE rss_user (
  id          SERIAL NOT NULL,
  email       VARCHAR NOT NULL,
  create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time TIMESTAMP WITH TIME ZONE,

  -- Ensure lowercase prior to insert.
  UNIQUE (email),
  PRIMARY KEY (id)
);

CREATE TRIGGER biu_rss_user
BEFORE INSERT OR UPDATE ON rss_user
FOR EACH ROW EXECUTE PROCEDURE trigger_lowercase_email();

CREATE TRIGGER bu_rss_user
BEFORE UPDATE ON rss_user
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

-- The state an rss_item may be in.
CREATE TYPE read_state AS ENUM ('unread', 'read', 'read-later');

-- Each user can flag an rss item as being in one state.
-- We assume if an item is not in this table that it is unread.
CREATE TABLE rss_item_state (
  id          SERIAL NOT NULL,
  state       read_state NOT NULL,
  item_id     INTEGER NOT NULL REFERENCES rss_item(id)
              ON DELETE CASCADE ON UPDATE CASCADE,
  user_id     INTEGER NOT NULL REFERENCES rss_user(id)
              ON DELETE CASCADE ON UPDATE CASCADE,
  create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time TIMESTAMP WITH TIME ZONE,
  UNIQUE (item_id, user_id),
  PRIMARY KEY (id)
);

CREATE TRIGGER bu_rss_item_state
BEFORE UPDATE ON rss_item_state
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

CREATE INDEX ON rss_item_state (item_id);

-- Table to hold items we mark read after having archived them. This means
-- that they were more interesting and were probably clicked and read. Record
-- them here to be able to refer to them more easily in the future.
CREATE TABLE rss_item_read_after_archive (
  id          SERIAL NOT NULL,
  user_id     INTEGER NOT NULL REFERENCES rss_user(id)
              ON DELETE CASCADE ON UPDATE CASCADE,
  -- I choose to not have these be foreign keys. This is primarily because I
  -- want to be able to periodically clear out the rss_item table (to keep it
  -- from getting huge).
  -- TODO(will@summercat.com): This is probably not appropriate any more. I did
  -- this when I was moving older items to rss_item_archive which I've now
  -- dropped.
  rss_feed_id INTEGER NOT NULL,
  rss_item_id INTEGER NOT NULL,
  create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time TIMESTAMP WITH TIME ZONE,

  UNIQUE (user_id, rss_feed_id, rss_item_id),
  PRIMARY KEY(id)
);

CREATE TRIGGER bu_rss_item_read_after_archive
BEFORE UPDATE ON rss_item_read_after_archive
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();


CREATE INDEX ON rss_item (publication_date);
