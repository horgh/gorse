--
-- In this upgrade I start tracking item states per user. I also introduce a new
-- state: Read later.
--
-- I also add an archive table and move old items there, and add create/update
-- time columns to rss_feed and rss_item.
--

-- A user. Each user subscribes to feeds.
CREATE TABLE rss_user (
  id SERIAL NOT NULL,
  email VARCHAR NOT NULL,
  create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time TIMESTAMP WITH TIME ZONE,
  -- Ensure lowercase prior to insert.
  UNIQUE(email),
  PRIMARY KEY (id)
);

CREATE FUNCTION trigger_lowercase_email()
RETURNS TRIGGER
AS $$
BEGIN
  NEW.email = LOWER(email);
  return NEW;
END
$$
LANGUAGE plpgsql;

CREATE TRIGGER biu_rss_user
BEFORE INSERT OR UPDATE ON rss_user
FOR EACH ROW EXECUTE PROCEDURE trigger_lowercase_email();

CREATE FUNCTION trigger_set_update_time()
RETURNS TRIGGER
AS $$
BEGIN
  NEW.update_time = NOW();
  return NEW;
END
$$
LANGUAGE plpgsql;

CREATE TRIGGER bu_rss_user
BEFORE UPDATE ON rss_user
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

-- The state an rss_item may be in.
CREATE TYPE read_state AS ENUM ('unread', 'read', 'read-later');

-- Each user can flag an rss item as being in one state.
-- We assume if an item is not in this table that it is unread.
CREATE TABLE rss_item_state (
  id SERIAL NOT NULL,
  state read_state NOT NULL,
  item_id INTEGER NOT NULL REFERENCES rss_item(id)
    ON DELETE CASCADE ON UPDATE CASCADE,
  user_id INTEGER NOT NULL REFERENCES rss_user(id)
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


-- Carry over read flags. Assign all to a new user.

INSERT INTO rss_user (email) VALUES('will@summercat.com');

INSERT INTO rss_item_state
(state, item_id, user_id)
SELECT 'read', ri.id, (SELECT id FROM rss_user
  WHERE email = 'will@summercat.com')
FROM rss_item ri
WHERE ri.read = TRUE;

ALTER TABLE rss_item DROP COLUMN read;


-- Move old rss_item entries here.
CREATE TABLE rss_item_archive (
  id INTEGER NOT NULL,
  title VARCHAR NOT NULL,
  description VARCHAR NOT NULL,
  link VARCHAR NOT NULL,
  publication_date TIMESTAMP WITHOUT TIME ZONE NOT NULL,
  rss_feed_id INTEGER NOT NULL REFERENCES rss_feed(id)
    ON UPDATE CASCADE ON DELETE CASCADE,
  UNIQUE(rss_feed_id, link)
);


-- Add create/update times to rss_feed and rss_item

ALTER TABLE rss_feed ADD COLUMN create_time TIMESTAMP WITH TIME ZONE
  NOT NULL DEFAULT NOW();
ALTER TABLE rss_feed ADD COLUMN update_time TIMESTAMP WITH TIME ZONE;

CREATE TRIGGER biu_rss_feed
BEFORE UPDATE ON rss_feed
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

ALTER TABLE rss_item ADD COLUMN create_time TIMESTAMP WITH TIME ZONE
  NOT NULL DEFAULT NOW();
ALTER TABLE rss_item ADD COLUMN update_time TIMESTAMP WITH TIME ZONE;

CREATE TRIGGER biu_rss_item
BEFORE UPDATE ON rss_item
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();

UPDATE rss_item SET create_time = publication_date;

ALTER TABLE rss_item_archive ADD COLUMN create_time TIMESTAMP WITH TIME ZONE
  NOT NULL DEFAULT NOW();
ALTER TABLE rss_item_archive ADD COLUMN update_time TIMESTAMP WITH TIME ZONE;

-- No trigger on archive. Just take the update time from the original table.


-- Archive anything older than X months that is read.

INSERT INTO rss_item_archive
(id, title, description, link, publication_date, rss_feed_id, create_time,
  update_time)
SELECT ri.id, ri.title, ri.description, ri.link, ri.publication_date,
  ri.rss_feed_id, ri.create_time, ri.update_time
FROM rss_item ri
LEFT JOIN rss_item_state ris ON ris.item_id = ri.id
WHERE ri.publication_date < NOW() - '1 months'::INTERVAL AND
COALESCE(ris.state, 'unread') = 'read';

DELETE FROM rss_item ri
WHERE
ri.id IN (SELECT ris.item_id FROM rss_item_state ris WHERE
  ris.item_id = ri.id AND ris.state = 'read') AND
ri.publication_date < NOW() - '1 months'::INTERVAL;


-- Table to hold items we mark read after having archived them. This means
-- that they were more interesting and were probably clicked and read. Record
-- them here to be able to refer to them more easily in the future.
CREATE TABLE rss_item_read_after_archive(
  id SERIAL NOT NULL,
  user_id INTEGER NOT NULL REFERENCES rss_user(id)
    ON DELETE CASCADE ON UPDATE CASCADE,
  -- I choose to not have these be foreign keys. This is primarily because I
  -- want to be able to periodically clear out the rss_item table (to keep it
  -- from getting huge).
  rss_feed_id INTEGER NOT NULL,
  rss_item_id INTEGER NOT NULL,
  create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  update_time TIMESTAMP WITH TIME ZONE,
  UNIQUE(user_id, rss_feed_id, rss_item_id),
  PRIMARY KEY(id)
);

CREATE TRIGGER bu_rss_item_read_after_archive
BEFORE UPDATE ON rss_item_read_after_archive
FOR EACH ROW EXECUTE PROCEDURE trigger_set_update_time();
