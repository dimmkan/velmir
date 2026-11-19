CREATE TABLE authors (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name character varying(200) NOT NULL,
    bio text,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE TABLE books (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id bigint NOT NULL REFERENCES authors(id),
    title character varying(255) NOT NULL,
    isbn character varying(20),
    published_on date,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE TABLE categories (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name character varying(100) NOT NULL
);

CREATE TABLE book_categories (
    book_id bigint NOT NULL REFERENCES books(id),
    category_id bigint NOT NULL REFERENCES categories(id),
    note text,
    PRIMARY KEY (book_id, category_id)
);

CREATE VIEW books_overview AS
SELECT
    books.id,
    books.title,
    authors.name AS author_name,
    books.published_on
FROM books
JOIN authors ON authors.id = books.author_id;

INSERT INTO authors (name, bio, created_at) VALUES
    ('Alice', 'PostgreSQL engineer', '2024-01-01T10:00:00Z'),
    ('Bob', NULL, '2024-01-02T10:00:00Z'),
    ('Carol', 'Technical writer', '2024-01-03T10:00:00Z');

INSERT INTO books (author_id, title, isbn, published_on, metadata, created_at)
SELECT id, 'PostgreSQL Guide', '978-1-1111-1111-1', DATE '2024-02-01', JSONB '{"edition":1,"big":9223372036854775807}', TIMESTAMPTZ '2024-02-01T10:00:00Z' FROM authors WHERE name = 'Alice'
UNION ALL
SELECT id, 'SQL Basics', '978-2-2222-2222-2', DATE '2024-01-15', JSONB '{"edition":1}', TIMESTAMPTZ '2024-02-02T10:00:00Z' FROM authors WHERE name = 'Bob'
UNION ALL
SELECT id, 'Advanced Queries', NULL, NULL, JSONB '{"edition":2,"tags":["sql","postgres"]}', TIMESTAMPTZ '2024-02-03T10:00:00Z' FROM authors WHERE name = 'Carol'
UNION ALL
SELECT id, 'PostgreSQL Cookbook', '978-3-3333-3333-3', DATE '2024-03-01', JSONB '{"edition":3}', TIMESTAMPTZ '2024-02-04T10:00:00Z' FROM authors WHERE name = 'Alice';

CREATE ROLE velmir_readonly LOGIN PASSWORD 'velmir_readonly_local_only';
ALTER ROLE velmir_readonly SET default_transaction_read_only = on;
GRANT CONNECT ON DATABASE velmir_demo TO velmir_readonly;
GRANT USAGE ON SCHEMA public TO velmir_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO velmir_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO velmir_readonly;

CREATE ROLE velmir_writer LOGIN PASSWORD 'velmir_writer_local_only';
GRANT CONNECT ON DATABASE velmir_demo TO velmir_writer;
GRANT USAGE ON SCHEMA public TO velmir_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO velmir_writer;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO velmir_writer;
