-- What a report was asked for: the language of its text and the AI model
-- (empty = the configured default). A cached report is only reused for the
-- same language and model.
ALTER TABLE reports ADD COLUMN lang  text NOT NULL DEFAULT 'en';
ALTER TABLE reports ADD COLUMN model text NOT NULL DEFAULT '';
