ALTER TABLE request_events ADD CONSTRAINT request_events_request_id_fkey
    FOREIGN KEY (request_id) REFERENCES requests(id);
