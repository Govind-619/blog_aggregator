-- +goose Up
CREATE TABLE posts (
    id UUID PRIMARY KEY,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    description TEXT,
    published_at TIMESTAMP,
    feed_id UUID NOT NULL REFERENCES feeds(id) ON DELETE CASCADE
);

CREATE INDEX idx_posts_feed_id ON posts(feed_id);
CREATE INDEX idx_posts_published_at ON posts(published_at DESC);

-- +goose Down
DROP TABLE posts;
