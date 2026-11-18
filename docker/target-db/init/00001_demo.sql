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

CREATE ROLE velmir_readonly LOGIN PASSWORD 'velmir_readonly_local_only';
ALTER ROLE velmir_readonly SET default_transaction_read_only = on;
GRANT CONNECT ON DATABASE velmir_demo TO velmir_readonly;
GRANT USAGE ON SCHEMA public TO velmir_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO velmir_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO velmir_readonly;
