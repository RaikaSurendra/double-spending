CREATE TABLE IF NOT EXISTS accounts (
    id      INT     PRIMARY KEY,
    balance INT     NOT NULL DEFAULT 0,
    version INT     NOT NULL DEFAULT 0
);
