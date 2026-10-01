-- Whether a feed's site homepage has been read for a <link rel="icon"> yet
-- (#451). Some sites serve an empty /favicon.ico and declare their real icon
-- only on the homepage, so the poller's /favicon.ico guess is not always
-- enough. Adding a feed by its feed URL reads the homepage once; for feeds
-- that are already stuck on a dead guess, the poller does it once more and
-- sets this flag either way, so a site with no usable icon at all is not
-- re-fetched on every poll.
ALTER TABLE reader_feeds ADD COLUMN favicon_page_checked INTEGER NOT NULL DEFAULT 0;
